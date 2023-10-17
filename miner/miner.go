package miner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/filecoin-project/lotus/monitor"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace/spans"
	"os"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	lrand "github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/journal"


	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/gwaylib/errors"
)

var log = logging.Logger("miner")

// Journal event types.
const (
	evtTypeBlockMined = iota
)

// waitFunc is expected to pace block mining at the configured network rate.
//
// baseTime is the timestamp of the mining base, i.e. the timestamp
// of the tipset we're planning to construct upon.
//
// Upon each mining loop iteration, the returned callback is called reporting
// whether we mined a block in this round or not.
type waitFunc func(ctx context.Context, baseTime uint64) (func(bool, abi.ChainEpoch, error), abi.ChainEpoch, error)

func randTimeOffset(width time.Duration) time.Duration {
	buf := make([]byte, 8)
	rand.Reader.Read(buf) //nolint:errcheck
	val := time.Duration(binary.BigEndian.Uint64(buf) % uint64(width))

	return val - (width / 2)
}

// NewMiner instantiates a miner with a concrete WinningPoStProver and a miner
// address (which can be different from the worker's address).
func NewMiner(api v1api.FullNode, epp gen.WinningPoStProver, addr address.Address, sf *slashfilter.SlashFilter, j journal.Journal) *Miner {
	arc, err := lru.NewARC[abi.ChainEpoch, bool](10000)
	if err != nil {
		panic(err)
	}

	//此处和storage.miner的new方法都会执行monitor.Init(once保证了monitor只会初始化一次)
	monitor.Init(model.PackageKind_Miner, addr.String())
	return &Miner{
		api:     api,
		epp:     epp,
		address: addr,
		waitFunc: func(ctx context.Context, baseTime uint64) (func(bool, abi.ChainEpoch, error), abi.ChainEpoch, error) {
			// wait around for half the block time in case other parents come in
			//
			// if we're mining a block in the past via catch-up/rush mining,
			// such as when recovering from a network halt, this sleep will be
			// for a negative duration, and therefore **will return
			// immediately**.
			//
			// the result is that we WILL NOT wait, therefore fast-forwarding
			// and thus healing the chain by backfilling it with null rounds
			// rapidly.
			deadline := baseTime + build.PropagationDelaySecs
			baseT := time.Unix(int64(deadline), 0)

			baseT = baseT.Add(randTimeOffset(time.Second))

			build.Clock.Sleep(build.Clock.Until(baseT))

			return func(bool, abi.ChainEpoch, error) {}, 0, nil
		},

		sf:                sf,
		minedBlockHeights: arc,
		evtTypes: [...]journal.EventType{
			evtTypeBlockMined: j.RegisterEventType("miner", "block_mined"),
		},
		journal: j,
	}
}

// Miner encapsulates the mining processes of the system.
//
// Refer to the godocs on mineOne and mine methods for more detail.
type Miner struct {
	api v1api.FullNode

	epp gen.WinningPoStProver

	lk       sync.Mutex
	address  address.Address
	stop     chan struct{}
	stopping chan struct{}

	waitFunc waitFunc

	// lastWork holds the last MiningBase we built upon.
	lastWork *MiningBase

	sf *slashfilter.SlashFilter
	// minedBlockHeights is a safeguard that caches the last heights we mined.
	// It is consulted before publishing a newly mined block, for a sanity check
	// intended to avoid slashings in case of a bug.
	minedBlockHeights *lru.ARCCache[abi.ChainEpoch, bool]

	evtTypes [1]journal.EventType
	journal  journal.Journal
}

// Address returns the address of the miner.
func (m *Miner) Address() address.Address {
	m.lk.Lock()
	defer m.lk.Unlock()

	return m.address
}

// Start starts the mining operation. It spawns a goroutine and returns
// immediately. Start is not idempotent.
func (m *Miner) Start(_ context.Context) error {
	m.lk.Lock()
	defer m.lk.Unlock()
	if m.stop != nil {
		return fmt.Errorf("miner already started")
	}
	m.stop = make(chan struct{})
	go m.mine(context.TODO())
	return nil
}

// Stop stops the mining operation. It is not idempotent, and multiple adjacent
// calls to Stop will fail.
func (m *Miner) Stop(ctx context.Context) error {
	m.lk.Lock()

	m.stopping = make(chan struct{})
	stopping := m.stopping
	close(m.stop)

	m.lk.Unlock()

	select {
	case <-stopping:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Miner) niceSleep(d time.Duration) bool {
	select {
	case <-build.Clock.After(d):
		return true
	case <-m.stop:
		log.Infow("received interrupt while trying to sleep in mining cycle")
		return false
	}
}
func nextRoundTime(base *MiningBase) time.Time {
	return time.Unix(int64(base.TipSet.MinTimestamp())+int64(build.BlockDelaySecs)*int64((base.NullRounds+1)), 0)
}

// mine runs the mining loop. It performs the following:
//
//  1. Queries our current best currently-known mining candidate (tipset to
//     build upon).
//  2. Waits until the propagation delay of the network has elapsed (currently
//     6 seconds). The waiting is done relative to the timestamp of the best
//     candidate, which means that if it's way in the past, we won't wait at
//     all (e.g. in catch-up or rush mining).
//  3. After the wait, we query our best mining candidate. This will be the one
//     we'll work with.
//  4. Sanity check that we _actually_ have a new mining base to mine on. If
//     not, wait one epoch + propagation delay, and go back to the top.
//  5. We attempt to mine a block, by calling mineOne (refer to godocs). This
//     method will either return a block if we were eligible to mine, or nil
//     if we weren't.
//     6a. If we mined a block, we update our state and push it out to the network
//     via gossipsub.
//     6b. If we didn't mine a block, we consider this to be a nil round on top of
//     the mining base we selected. If other miner or miners on the network
//     were eligible to mine, we will receive their blocks via gossipsub and
//     we will select that tipset on the next iteration of the loop, thus
//     discarding our null round.
func (m *Miner) mine(ctx context.Context) {
	go m.doWinPoStWarmup(ctx)

	var lastBase MiningBase
	var nextRound time.Time

	for {
		ctx := cliutil.OnSingleNode(ctx)

		select {
		case <-m.stop:
			stopping := m.stopping
			m.stop = nil
			m.stopping = nil
			close(stopping)
			return

		default:
		}
	miningBegin:

		oldbase := lastBase
		prebase, err := m.GetBestMiningCandidate(ctx)
		if err != nil {
			log.Errorf("failed to get best mining candidate: %s", err)
			m.niceSleep(time.Second * 1)
			continue
		}
		miningHeight := prebase.TipSet.Height() + 1
		if lastBase.TipSet != nil && lastBase.TipSet.Height()+lastBase.NullRounds > prebase.TipSet.Height() {
			miningHeight = lastBase.TipSet.Height() + lastBase.NullRounds + 1
		}
		// just wait for the beacon entry to become available before we select our final mining base
		_, err = m.api.StateGetBeaconEntry(ctx, miningHeight)
		if err != nil {
			log.Errorf("failed getting beacon entry: %s", err)
			m.niceSleep(time.Second * 1)
			continue
		}
		if !prebase.TipSet.Equals(lastBase.TipSet) {

			// log the win packing has on a right chain.
			lastBlks := prebase.TipSet.Blocks()
			for _, blk := range lastBlks {
				if blk.Miner.String() != m.address.String() {
					continue
				}
				if err := database.AddWinSuc(time.Unix(int64(blk.Timestamp), 0)); err != nil {
					log.Error(errors.As(err))
				}
				break
			}

			base := prebase
			// cause by net delay, skiping for a late tipset in begining of genesis node.
			now := time.Now()
			// 3/2 = 1.5
			// 5/2 = 2.5
			// 7/2 = 3.5
			delay := (time.Duration(build.PropagationDelaySecs*7/2) * time.Second) - now.Sub(nextRound)
			log.Infof("Waiting PropagationDelay time: %s", delay)
			if delay > 0 && now.Sub(nextRound) > 0 {
				time.Sleep(delay + (time.Second / 2)) // offset 500ms
				// update the parent weight
				base, err = m.GetBestMiningCandidate(ctx)
				if err != nil {
					log.Errorf("failed to get best mining candidate: %s", err)
					continue
				}
			}

			//log.Infof("Update %s base height to: %d", m.address, base.TipSet.Height())

			nextRound = nextRoundTime(base)
			lastBase = *base
		} else {
			now := time.Now()
			// if the base was dead, make the nullRound++ step by round actually change.
			// and in current round, checking the base by every 1 second until pass or round out.
			if lastBase.TipSet == nil || (now.Unix() < nextRound.Unix()+int64(2*build.PropagationDelaySecs)) {
				time.Sleep(1e9)
				continue
			} else if now.Unix() > (nextRound.Unix() + int64(build.BlockDelaySecs)) {
				// slowly down to mining a null round.
				time.Sleep(time.Duration(build.PropagationDelaySecs) * 1e9)
			}

		syncLoop:
			syncing := false
			progress, err := m.api.SyncProgress(ctx)
			if err == nil {
				if progress.Syncing {
					syncing = true
					log.Infof("Waiting chain sync, current:%d, target:%d",
						progress.VerifyHeight,
						progress.TargetHeight,
					)
					time.Sleep(5e9)
					goto syncLoop
				}
				if syncing {
					// when sync done, goto mining again
					goto miningBegin
				}
			}

			log.Infof("BestMiningCandidate from the previous(%d) round: %s (nulls:%d)", lastBase.TipSet.Height(), lastBase.TipSet.Cids(), lastBase.NullRounds)
			lastBase.NullRounds++
			nextRound = nextRoundTime(&lastBase)
		}

		//log.Infof("Trying mineOne")
		var span *spans.MineSpan
		block := make(chan interface{}, 1)
		mineCtx, mineCtxCancel := context.WithCancel(ctx)
		mineCtx, span = spans.NewMineSpan(mineCtx)
		go func() {
			span.Starting("")
			took, b, err := m.mineOne(mineCtx, &oldbase, &lastBase, nextRound, span)
			if err != nil {
				span.Finish(err)
				if err := database.AddWinErr(nextRound); err != nil {
					log.Warn(errors.As(err))
				}
				block <- err
			} else {
				if b != nil {
					if err := database.AddWinGen(nextRound, took); err != nil {
						log.Warn(errors.As(err))
					}
				}
				block <- b
			}
		}()

		var b *types.BlockMsg
		select {
		case bl := <-block:
			mineCtxCancel()
			err, ok := bl.(error)
			if ok {
				log.Errorf("mining block failed: %+v", err)
				m.niceSleep(time.Second)
				//onDone(false, 0, err)
				continue
			}
			b = bl.(*types.BlockMsg)
		case <-build.Clock.After(time.Duration(build.BlockDelaySecs) * time.Second):
			err := errors.New("mining block failed by timeout, does the wallet undecode or compute timeout?")
			mineCtxCancel()
			log.Error(err)
			span.Finish(err)
			continue
		}
		//lastBase = *base

		//var h abi.ChainEpoch
		//if b != nil {
		//	h = b.Header.Height
		//}
		//onDone(b != nil, h, nil)

		if b != nil {
			m.journal.RecordEvent(m.evtTypes[evtTypeBlockMined], func() interface{} {
				return map[string]interface{}{
					"parents":   lastBase.TipSet.Cids(),
					"nulls":     lastBase.NullRounds,
					"epoch":     b.Header.Height,
					"timestamp": b.Header.Timestamp,
					"cid":       b.Header.Cid(),
				}
			})

			btime := time.Unix(int64(b.Header.Timestamp), 0)
			now := build.Clock.Now()
			switch {
			case btime == now:
				// block timestamp is perfectly aligned with time.
			case btime.After(now):
				if !m.niceSleep(build.Clock.Until(btime)) {
					log.Warnf("received interrupt while waiting to broadcast block, will shutdown after block is sent out")
					build.Clock.Sleep(build.Clock.Until(btime))
				}
			default:
				log.Warnw("mined block in the past",
					"block-time", btime, "time", build.Clock.Now(), "difference", build.Clock.Since(btime))
			}

			if os.Getenv("LOTUS_MINER_NO_SLASHFILTER") != "_yes_i_know_i_can_and_probably_will_lose_all_my_fil_and_power_" {
				witness, fault, err := m.sf.MinedBlock(ctx, b.Header, base.TipSet.Height()+base.NullRounds)
				if err != nil {
					log.Errorf("<!!> SLASH FILTER ERRORED: %s", err)
					// Continue here, because it's _probably_ wiser to not submit this block
					continue
				}

				if fault {
					log.Errorf("<!!> SLASH FILTER DETECTED FAULT due to blocks %s and %s", b.Header.Cid(), witness)
					continue
				}
				lastBase.TipSet = nil // clean the cache and redo the mining
				continue
			}

			if _, ok := m.minedBlockHeights.Get(b.Header.Height); ok {
				log.Warnw("Created a block at the same height as another block we've created", "height", b.Header.Height, "miner", b.Header.Miner, "parents", b.Header.Parents)
				continue
			}

			m.minedBlockHeights.Add(b.Header.Height, true)

			for i := 0; i < 10; i++ {
				if err := m.api.SyncSubmitBlock(ctx, b); err != nil {
					log.Errorf("failed to submit newly mined block: %+v", err)
					time.Sleep(1e9)
					continue
				}
				// submit success, break
				break
			}
			if err != nil {
				span.SetBlockCount(0)
				span.Finish(err)
			} else {
				span.SetBlockCount(1)
				span.Finish(nil)
			}
		} else {
			span.SetBlockCount(0)
			span.Finish(nil)
			// Wait until the next epoch, plus the propagation delay, so a new tipset
			// has enough time to form.
			//
			// See:  https://github.com/filecoin-project/lotus/issues/1845
			//nextRound := time.Unix(int64(base.TipSet.MinTimestamp()+build.BlockDelaySecs*uint64(base.NullRounds))+int64(build.PropagationDelaySecs), 0)

			log.Info("mine next round at:", nextRound.Format(time.RFC3339))

			select {
			case <-build.Clock.After(build.Clock.Until(nextRound)):
			case <-m.stop:
				stopping := m.stopping
				m.stop = nil
				m.stopping = nil
				close(stopping)
				return
			}
		}
	}
}

// MiningBase is the tipset on top of which we plan to construct our next block.
// Refer to godocs on GetBestMiningCandidate.
type MiningBase struct {
	TipSet     *types.TipSet
	NullRounds abi.ChainEpoch
}

// GetBestMiningCandidate implements the fork choice rule from a miner's
// perspective.
//
// It obtains the current chain head (HEAD), and compares it to the last tipset
// we selected as our mining base (LAST). If HEAD's weight is larger than
// LAST's weight, it selects HEAD to build on. Else, it selects LAST.
func (m *Miner) GetBestMiningCandidate(ctx context.Context) (*MiningBase, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	bts, err := m.api.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	if m.lastWork != nil {
		if m.lastWork.TipSet.Equals(bts) {
			return m.lastWork, nil
		}

		btsw, err := m.api.ChainTipSetWeight(ctx, bts.Key())
		if err != nil {
			m.lastWork = nil
			return nil, err
		}
		ltsw, err := m.api.ChainTipSetWeight(ctx, m.lastWork.TipSet.Key())
		if err != nil {
			m.lastWork = nil
			return nil, err
		}

		if types.BigCmp(btsw, ltsw) <= 0 {
			return m.lastWork, nil
		}
	}

	m.lastWork = &MiningBase{TipSet: bts}
	return m.lastWork, nil
}

// mineOne attempts to mine a single block, and does so synchronously, if and
// only if we are eligible to mine.
//
// {hint/landmark}: This method coordinates all the steps involved in mining a
// block, including the condition of whether mine or not at all depending on
// whether we win the round or not.
//
// This method does the following:
//
//  1.
func (m *Miner) mineOne(ctx context.Context, oldbase, base *MiningBase, submitTime time.Time, span *spans.MineSpan) (dur time.Duration, minedBlock *types.BlockMsg, err error) {
	log.Debugw("attempting to mine a block", "tipset", types.LogCids(base.TipSet.Cids()))
	tStart := build.Clock.Now()

	round := base.TipSet.Height() + base.NullRounds + 1
	span.SetRound(int64(round))

	// always write out a log
	var winner *types.ElectionProof
	var mbi *api.MiningBaseInfo
	var rbase types.BeaconEntry
	defer func() {

		var hasMinPower bool

		// mbi can be nil if we are deep in penalty and there are 0 eligible sectors
		// in the current deadline. If this case - put together a dummy one for reporting
		// https://github.com/filecoin-project/lotus/blob/v1.9.0/chain/stmgr/utils.go#L500-L502
		if mbi == nil {
			mbi = &api.MiningBaseInfo{
				NetworkPower:      big.NewInt(-1), // we do not know how big the network is at this point
				EligibleForMining: false,
				MinerPower:        big.NewInt(0), // but we do know we do not have anything eligible
			}

			// try to opportunistically pull actual power and plug it into the fake mbi
			if pow, err := m.api.StateMinerPower(ctx, m.address, base.TipSet.Key()); err == nil && pow != nil {
				hasMinPower = pow.HasMinPower
				mbi.MinerPower = pow.MinerPower.QualityAdjPower
				mbi.NetworkPower = pow.TotalPower.QualityAdjPower
			}
		}

		isLate := uint64(tStart.Unix()) > (base.TipSet.MinTimestamp() + uint64(base.NullRounds*builtin.EpochDurationSeconds) + build.PropagationDelaySecs)
		span.SetLate(isLate)
		span.SetNullRound(int64(base.NullRounds))
		span.SetLookbackEpoch(int64(policy.ChainFinality))
		span.SetBaseEpoch(int64(base.TipSet.Height()))
		span.SetBaseDeltaSeconds(float64(uint64(tStart.Unix()) - base.TipSet.MinTimestamp()))

		logStruct := []interface{}{
			"tookMilliseconds", (build.Clock.Now().UnixNano() - tStart.UnixNano()) / 1_000_000,
			"forRound", int64(round),
			"baseEpoch", int64(base.TipSet.Height()),
			"baseDeltaSeconds", uint64(tStart.Unix()) - base.TipSet.MinTimestamp(),
			"nullRounds", int64(base.NullRounds),
			"lateStart", isLate,
			"beaconEpoch", rbase.Round,
			"lookbackEpochs", int64(policy.ChainFinality), // hardcoded as it is unlikely to change again: https://github.com/filecoin-project/lotus/blob/v1.8.0/chain/actors/policy/policy.go#L180-L186
			"networkPowerAtLookback", mbi.NetworkPower.String(),
			"minerPowerAtLookback", mbi.MinerPower.String(),
			"isEligible", mbi.EligibleForMining,
			"isWinner", (winner != nil),
			"error", err,
		}

		if err != nil {
			log.Errorw("completed mineOne", logStruct...)
		} else if isLate || (hasMinPower && !mbi.EligibleForMining) {
			log.Warnw("completed mineOne", logStruct...)
		} else {
			log.Infow("completed mineOne", logStruct...)
		}
	}()

	mbiStart := time.Now()
	mbi, err = m.api.MinerGetBaseInfo(ctx, m.address, round, base.TipSet.Key())
	span.SetBaseInfoDuration(time.Now().Sub(mbiStart).Milliseconds())
	if err != nil {
		err = xerrors.Errorf("failed to get mining base info: %w", err)
		return 0, nil, err
	}

	// log mineOne statis
	expNum := 0
	if mbi != nil {
		span.SetBaseInfoNil(false)
		span.SetTotalPower(mbi.NetworkPower.String())
		span.SetMinerPower(mbi.MinerPower.String())
		span.SetEligible(mbi.EligibleForMining)

		qpercI := types.BigDiv(types.BigMul(mbi.MinerPower, types.NewInt(1000000)), mbi.NetworkPower)
		expWinChance := float64(types.BigMul(qpercI, types.NewInt(build.BlocksPerEpoch)).Int64()) / 1000000
		if expWinChance > 1 {
			expWinChance = 1
		}
		expNum = int(float64(time.Hour*24/(time.Second*time.Duration(build.BlockDelaySecs))) * expWinChance)
	}
	if err := database.AddWinTimes(submitTime, expNum); err != nil {
		log.Warn(errors.As(err))
	}
	if mbi == nil {
		span.SetBaseInfoNil(true)
		return 0, nil, nil
	}

	if !mbi.EligibleForMining {
		// slashed or just have no power yet
		return 0, nil, nil
	}

	tPowercheck := build.Clock.Now()

	preHeight := abi.ChainEpoch(0)
	if oldbase.TipSet != nil {
		preHeight = oldbase.TipSet.Height()
	}
	log.Infof(
		"Time delta between now and our mining, base(%d|%d):%ds (nulls: %d), power(%s/%s)",
		preHeight, base.TipSet.Height(),
		uint64(build.Clock.Now().Unix())-base.TipSet.MinTimestamp(), base.NullRounds,
		mbi.MinerPower, mbi.NetworkPower,
	)

	bvals := mbi.BeaconEntries
	rbase = mbi.PrevBeaconEntry
	if len(bvals) > 0 {
		rbase = bvals[len(bvals)-1]
	}

	span.SetBeaconEpoch(int64(rbase.Round))
	span.SetBeacon(hex.EncodeToString(rbase.Data))
	ticket, err := m.computeTicket(ctx, &rbase, base, mbi)
	if err != nil {
		err = xerrors.Errorf("scratching ticket failed: %w", err)
		return 0, nil, err
	}

	winner, err = gen.IsRoundWinner(ctx, base.TipSet, round, m.address, rbase, mbi, m.api)
	if err != nil {
		err = xerrors.Errorf("failed to check if we win next round: %w", err)
		return 0, nil, err
	}

	if winner == nil {
		span.SetWinCount(0)
		return 0, nil, nil
	} else {
		span.SetWinCount(int(winner.WinCount))
	}

	tTicket := build.Clock.Now()

	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		err = xerrors.Errorf("failed to marshal miner address: %w", err)
		return 0, nil, err
	}

	rand, err := lrand.DrawRandomnessFromBase(rbase.Data, crypto.DomainSeparationTag_WinningPoStChallengeSeed, round, buf.Bytes())
	if err != nil {
		err = xerrors.Errorf("failed to get randomness for winning post: %w", err)
		return 0, nil, err
	}

	prand := abi.PoStRandomness(rand)

	tSeed := build.Clock.Now()
	nv, err := m.api.StateNetworkVersion(ctx, base.TipSet.Key())
	if err != nil {
		return 0, nil, err
	}

	postProof, err := m.epp.ComputeProof(ctx, mbi.Sectors, prand, round, nv)
	if err != nil {
		log.Warn(errors.As(err, mbi.Sectors, prand))
		err = xerrors.Errorf("failed to compute winning post proof: %w", err)
		return 0, nil, err
	}

	tProof := build.Clock.Now()

	// get pending messages early,
	msgs, err := m.api.MpoolSelect(context.TODO(), base.TipSet.Key(), ticket.Quality())
	if err != nil {
		err = xerrors.Errorf("failed to select messages for block: %w", err)
		return 0, nil, err
	}
	span.SetMsgCount(len(msgs))
	tPending := build.Clock.Now()

	// TODO: winning post proof
	minedBlock, err = m.createBlock(base, m.address, ticket, winner, bvals, postProof, msgs)
	if err != nil {
		err = xerrors.Errorf("failed to create block: %w", err)
		return 0, nil, err
	}

	tCreateBlock := build.Clock.Now()
	dur = tCreateBlock.Sub(tStart)
	parentMiners := make([]address.Address, len(base.TipSet.Blocks()))
	for i, header := range base.TipSet.Blocks() {
		parentMiners[i] = header.Miner
	}
	log.Infow("mined new block",
		"cid", minedBlock.Cid(),
		"height", minedBlock.Header.Height,
		"weight", minedBlock.Header.ParentWeight,
		"rounds", base.NullRounds,
		"took", dur,
		"miner", minedBlock.Header.Miner,
		"parents", parentMiners,
		"submit", time.Unix(int64(base.TipSet.MinTimestamp()+(uint64(base.NullRounds)+1)*build.BlockDelaySecs), 0).Format(time.RFC3339))
	if dur > time.Second*time.Duration(build.BlockDelaySecs) {
		log.Warnw("CAUTION: block production took longer than the block delay. Your computer may not be fast enough to keep up",
			"tPowercheck ", tPowercheck.Sub(tStart),
			"tTicket ", tTicket.Sub(tPowercheck),
			"tSeed ", tSeed.Sub(tTicket),
			"tProof ", tProof.Sub(tSeed),
			"tPending ", tPending.Sub(tProof),
			"tCreateBlock ", tCreateBlock.Sub(tPending))
	}

	return dur, minedBlock, nil
}

func (m *Miner) computeTicket(ctx context.Context, brand *types.BeaconEntry, base *MiningBase, mbi *api.MiningBaseInfo) (*types.Ticket, error) {
	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	round := base.TipSet.Height() + base.NullRounds + 1
	if round > build.UpgradeSmokeHeight {
		buf.Write(base.TipSet.MinTicket().VRFProof)
	}

	input, err := lrand.DrawRandomnessFromBase(brand.Data, crypto.DomainSeparationTag_TicketProduction, round-build.TicketRandomnessLookback, buf.Bytes())
	if err != nil {
		return nil, err
	}

	vrfOut, err := gen.ComputeVRF(ctx, m.api.WalletSign, mbi.WorkerKey, input)
	if err != nil {
		return nil, err
	}

	return &types.Ticket{
		VRFProof: vrfOut,
	}, nil
}

func (m *Miner) createBlock(base *MiningBase, addr address.Address, ticket *types.Ticket,
	eproof *types.ElectionProof, bvals []types.BeaconEntry, wpostProof []proof.PoStProof, msgs []*types.SignedMessage) (*types.BlockMsg, error) {
	uts := base.TipSet.MinTimestamp() + build.BlockDelaySecs*(uint64(base.NullRounds)+1)

	nheight := base.TipSet.Height() + base.NullRounds + 1

	// why even return this? that api call could just submit it for us
	return m.api.MinerCreateBlock(context.TODO(), &api.BlockTemplate{
		Miner:            addr,
		Parents:          base.TipSet.Key(),
		Ticket:           ticket,
		Eproof:           eproof,
		BeaconValues:     bvals,
		Messages:         msgs,
		Epoch:            nheight,
		Timestamp:        uts,
		WinningPoStProof: wpostProof,
	})
}
