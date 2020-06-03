package miner

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	lru "github.com/hashicorp/golang-lru"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"

	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/gwaylib/errors"
)

var log = logging.Logger("miner")

// returns a callback reporting whether we mined a blocks in this round
type waitFunc func(ctx context.Context, baseTime uint64) (func(bool), error)

func NewMiner(api api.FullNode, epp gen.WinningPoStProver, addr address.Address) *Miner {
	arc, err := lru.NewARC(10000)
	if err != nil {
		panic(err)
	}

	return &Miner{
		api:     api,
		epp:     epp,
		address: addr,
		waitFunc: func(ctx context.Context, baseTime uint64) (func(bool), error) {
			// Wait around for half the block time in case other parents come in
			deadline := baseTime + build.PropagationDelay
			time.Sleep(time.Until(time.Unix(int64(deadline), 0)))

			return func(bool) {}, nil
		},
		minedBlockHeights: arc,
	}
}

type Miner struct {
	api api.FullNode

	epp gen.WinningPoStProver

	lk       sync.Mutex
	address  address.Address
	stop     chan struct{}
	stopping chan struct{}

	waitFunc waitFunc

	lastWork *MiningBase

	minedBlockHeights *lru.ARCCache
}

func (m *Miner) Address() address.Address {
	m.lk.Lock()
	defer m.lk.Unlock()

	return m.address
}

func (m *Miner) Start(ctx context.Context) error {
	m.lk.Lock()
	defer m.lk.Unlock()
	if m.stop != nil {
		return fmt.Errorf("miner already started")
	}
	m.stop = make(chan struct{})
	go m.mine(context.TODO())
	return nil
}

func (m *Miner) Stop(ctx context.Context) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.stopping = make(chan struct{})
	stopping := m.stopping
	close(m.stop)

	select {
	case <-stopping:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Miner) niceSleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-m.stop:
		return false
	}
}

func nextRoundTime(base *MiningBase) time.Time {
	return time.Unix(int64(base.TipSet.MinTimestamp())+int64(build.BlockDelay*(base.NullRounds+1)), 0)
}
func (m *Miner) mine(ctx context.Context) {
	ctx, span := trace.StartSpan(ctx, "/mine")
	defer span.End()

	var lastBase MiningBase
	var nextRound time.Time

	for {
		select {
		case <-m.stop:
			stopping := m.stopping
			m.stop = nil
			m.stopping = nil
			close(stopping)
			return

		default:
		}

		prebase, err := m.GetBestMiningCandidate(ctx)
		if err != nil {
			log.Errorf("failed to get best mining candidate: %s", err)
			m.niceSleep(time.Second * 5)
			continue
		}

		//// Wait until propagation delay period after block we plan to mine on
		//onDone, err := m.waitFunc(ctx, prebase.TipSet.MinTimestamp())
		//if err != nil {
		//	log.Error(err)
		//	return
		//}

		now := time.Now()
		if lastBase.TipSet == nil || !prebase.TipSet.Equals(lastBase.TipSet) {
			// cause net delay, skip for in a late time.
			if int64(prebase.TipSet.MinTimestamp()) < now.Unix()-build.BlockDelay {
				time.Sleep(build.PropagationDelay)
			}
			base, err := m.GetBestMiningCandidate(ctx)
			if err != nil {
				log.Errorf("failed to get best mining candidate: %s", err)
				continue
			}
			nextRound = nextRoundTime(base)
			lastBase = *base
		} else {
			// if the base was dead, make the nullRound++ step by round actually change.
			if lastBase.TipSet == nil || (now.Unix()-nextRound.Unix())/build.BlockDelay == 0 {
				time.Sleep(1e9)
				continue
			}
			log.Infof("BestMiningCandidate from the previous(%d) round: %s (nulls:%d)", lastBase.TipSet.Height(), lastBase.TipSet.Cids(), lastBase.NullRounds)
			lastBase.NullRounds++
			nextRound = nextRoundTime(&lastBase)
		}

		//if base.TipSet.Equals(lastBase.TipSet) && lastBase.NullRounds == base.NullRounds {
		//	log.Warnf("BestMiningCandidate from the previous round: %s (nulls:%d)", lastBase.TipSet.Cids(), lastBase.NullRounds)
		//	m.niceSleep(build.BlockDelay * time.Second)
		//	continue
		//}
		//lastBase = *base

		b, err := m.mineOne(ctx, &lastBase)
		if err != nil {
			log.Errorf("mining block failed: %+v", err)
			continue
		}

		//onDone(b != nil)

		if b != nil {
			btime := time.Unix(int64(b.Header.Timestamp), 0)
			if time.Now().Before(btime) {
				if !m.niceSleep(time.Until(btime)) {
					log.Warnf("received interrupt while waiting to broadcast block, will shutdown after block is sent out")
					time.Sleep(time.Until(btime))
				}
			} else {
				log.Warnw("mined block in the past", "block-time", btime,
					"time", time.Now(), "duration", time.Since(btime))
			}

			// TODO: should do better 'anti slash' protection here
			blkKey := fmt.Sprintf("%d", b.Header.Height)
			if _, ok := m.minedBlockHeights.Get(blkKey); ok {
				log.Warnw("Created a block at the same height as another block we've created", "height", b.Header.Height, "miner", b.Header.Miner, "parents", b.Header.Parents)
				//continue
			}

			m.minedBlockHeights.Add(blkKey, true)
			if err := m.api.SyncSubmitBlock(ctx, b); err != nil {
				log.Errorf("failed to submit newly mined block: %s", err)
			}
		} else {
			// Wait until the next epoch, plus the propagation delay, so a new tipset
			// has enough time to form.
			//
			// See:  https://github.com/filecoin-project/lotus/issues/1845
			//nextRound := time.Unix(int64(base.TipSet.MinTimestamp()+uint64(build.BlockDelay*base.NullRounds))+int64(build.PropagationDelay), 0)

			log.Info("mine next round at:", nextRound.Format(time.RFC3339))
			select {
			case <-time.After(time.Until(nextRound)):
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

type MiningBase struct {
	TipSet     *types.TipSet
	NullRounds abi.ChainEpoch
}

func (m *Miner) GetBestMiningCandidate(ctx context.Context) (*MiningBase, error) {
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
			return nil, err
		}
		ltsw, err := m.api.ChainTipSetWeight(ctx, m.lastWork.TipSet.Key())
		if err != nil {
			return nil, err
		}

		if types.BigCmp(btsw, ltsw) <= 0 {
			return m.lastWork, nil
		}
	}

	m.lastWork = &MiningBase{TipSet: bts}
	return m.lastWork, nil
}

func (m *Miner) hasPower(ctx context.Context, addr address.Address, ts *types.TipSet) (bool, error) {
	mpower, err := m.api.StateMinerPower(ctx, addr, ts.Key())
	if err != nil {
		return false, err
	}

	return mpower.MinerPower.QualityAdjPower.GreaterThanEqual(power.ConsensusMinerMinPower), nil
}

func (m *Miner) mineOne(ctx context.Context, base *MiningBase) (*types.BlockMsg, error) {
	log.Debugw("attempting to mine a block", "tipset", types.LogCids(base.TipSet.Cids()))
	start := time.Now()

	// make auto clean for pending messages every round.
	pending, err := m.api.MpoolPending(context.TODO(), base.TipSet.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get pending messages: %w", err)
	}
	log.Infof("all pending msgs len:%d", len(pending))
	pending, err = SelectMessages(context.TODO(), m.api.StateGetActor, base.TipSet, &MsgPool{FromApi: m.api, Msgs: pending})
	if err != nil {
		return nil, xerrors.Errorf("message filtering failed: %w", err)
	}
	if len(pending) > build.BlockMessageLimit {
		log.Error("SelectMessages returned too many messages: ", len(pending))
		pending = pending[:build.BlockMessageLimit]
	}
	tPending := time.Now()

	round := base.TipSet.Height() + base.NullRounds + 1

	mbi, err := m.api.MinerGetBaseInfo(ctx, m.address, round, base.TipSet.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get mining base info: %w", err)
	}
	if mbi == nil {
		base.NullRounds++
		return nil, nil
	}

	tMBI := time.Now()

	beaconPrev := mbi.PrevBeaconEntry

	tDrand := time.Now()
	bvals := mbi.BeaconEntries

	hasPower, err := m.hasPower(ctx, m.address, base.TipSet)
	if err != nil {
		return nil, xerrors.Errorf("checking if miner is slashed: %w", err)
	}
	if !hasPower {
		// slashed or just have no power yet
		base.NullRounds++
		return nil, nil
	}

	tPowercheck := time.Now()

	log.Infof("Time delta between now and our mining base(%d): %ds (nulls: %d)", base.TipSet.Height(), uint64(time.Now().Unix())-base.TipSet.MinTimestamp(), base.NullRounds)

	rbase := beaconPrev
	if len(bvals) > 0 {
		rbase = bvals[len(bvals)-1]
	}

	ticket, err := m.computeTicket(ctx, &rbase, base, len(bvals) > 0)
	if err != nil {
		return nil, xerrors.Errorf("scratching ticket failed: %w", err)
	}

	winner, err := gen.IsRoundWinner(ctx, base.TipSet, round, m.address, rbase, mbi, m.api)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if we win next round: %w", err)
	}

	if winner == nil {
		log.Info("Not Win")
		base.NullRounds++
		return nil, nil
	}

	tTicket := time.Now()

	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal miner address: %w", err)
	}

	rand, err := store.DrawRandomness(rbase.Data, crypto.DomainSeparationTag_WinningPoStChallengeSeed, base.TipSet.Height()+base.NullRounds+1, buf.Bytes())
	if err != nil {
		return nil, xerrors.Errorf("failed to get randomness for winning post: %w", err)
	}

	prand := abi.PoStRandomness(rand)

	tSeed := time.Now()

	postProof, err := m.epp.ComputeProof(ctx, mbi.Sectors, prand)
	if err != nil {
		return nil, xerrors.Errorf("failed to compute winning post proof: %w", err)
	}

	// TODO: winning post proof
	b, err := m.createBlock(base, m.address, ticket, winner, bvals, postProof, pending)
	if err != nil {
		return nil, xerrors.Errorf("failed to create block: %w", err)
	}

	tCreateBlock := time.Now()
	dur := tCreateBlock.Sub(start)
	log.Infow("mined new block",
		"cid", b.Cid(),
		"height", b.Header.Height,
		"weight", b.Header.ParentWeight,
		"rounds", base.NullRounds,
		"took", dur,
		"submit", time.Unix(int64(base.TipSet.MinTimestamp()+(uint64(base.NullRounds)+1)*build.BlockDelay), 0).Format(time.RFC3339))
	if dur > time.Second*build.BlockDelay {
		log.Warn("CAUTION: block production took longer than the block delay. Your computer may not be fast enough to keep up")

		log.Warnw("tPending ", "duration", tPending.Sub(start))
		log.Warnw("tMinerBaseInfo ", "duration", tMBI.Sub(tPending))
		log.Warnw("tDrand ", "duration", tDrand.Sub(tMBI))
		log.Warnw("tPowercheck ", "duration", tPowercheck.Sub(tDrand))
		log.Warnw("tTicket ", "duration", tTicket.Sub(tPowercheck))
		log.Warnw("tSeed ", "duration", tSeed.Sub(tTicket))
		log.Warnw("tCreateBlock ", "duration", tCreateBlock.Sub(tSeed))
	}

	return b, nil
}

func (m *Miner) computeTicket(ctx context.Context, brand *types.BeaconEntry, base *MiningBase, haveNewEntries bool) (*types.Ticket, error) {
	mi, err := m.api.StateMinerInfo(ctx, m.address, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	worker, err := m.api.StateAccountKey(ctx, mi.Worker, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	if !haveNewEntries {
		buf.Write(base.TipSet.MinTicket().VRFProof)
	}

	input, err := store.DrawRandomness(brand.Data, crypto.DomainSeparationTag_TicketProduction, base.TipSet.Height()+base.NullRounds+1-build.TicketRandomnessLookback, buf.Bytes())
	if err != nil {
		return nil, err
	}

	vrfOut, err := gen.ComputeVRF(ctx, m.api.WalletSign, worker, input)
	if err != nil {
		return nil, err
	}

	return &types.Ticket{
		VRFProof: vrfOut,
	}, nil
}

func (m *Miner) createBlock(base *MiningBase, addr address.Address, ticket *types.Ticket,
	eproof *types.ElectionProof, bvals []types.BeaconEntry, wpostProof []abi.PoStProof, msgs []*types.SignedMessage) (*types.BlockMsg, error) {
	uts := base.TipSet.MinTimestamp() + uint64(build.BlockDelay*(base.NullRounds+1))
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

type actCacheEntry struct {
	act *types.Actor
	err error
}

type cachedActorLookup struct {
	tsk      types.TipSetKey
	cache    map[address.Address]actCacheEntry
	fallback ActorLookup
}

func (c *cachedActorLookup) StateGetActor(ctx context.Context, a address.Address, tsk types.TipSetKey) (*types.Actor, error) {
	if c.tsk == tsk {
		e, has := c.cache[a]
		if has {
			return e.act, e.err
		}
	}

	e, err := c.fallback(ctx, a, tsk)
	if c.tsk == tsk {
		c.cache[a] = actCacheEntry{
			act: e, err: err,
		}
	}
	return e, err
}

type ActorLookup func(context.Context, address.Address, types.TipSetKey) (*types.Actor, error)

func countFrom(msgs []*types.SignedMessage, from address.Address) (out int) {
	for _, msg := range msgs {
		if msg.Message.From == from {
			out++
		}
	}
	return out
}

type MsgPool struct {
	FromApi api.FullNode
	Msgs    []*types.SignedMessage
}

func (p *MsgPool) Remove(ctx context.Context, msg *types.SignedMessage) {
	// TODO: remove fault message
	if err := p.FromApi.MpoolRemove(ctx, msg.Message.From, msg.Message.Nonce); err != nil {
		log.Warn(errors.As(err))
	}
}

func SelectMessages(ctx context.Context, al ActorLookup, ts *types.TipSet, mpool *MsgPool) ([]*types.SignedMessage, error) {
	al = (&cachedActorLookup{
		tsk:      ts.Key(),
		cache:    map[address.Address]actCacheEntry{},
		fallback: al,
	}).StateGetActor

	out := make([]*types.SignedMessage, 0, build.BlockMessageLimit)
	inclNonces := make(map[address.Address]uint64)
	inclBalances := make(map[address.Address]types.BigInt)
	inclCount := make(map[address.Address]int)

	tooLowFundMsgs := 0
	tooHighNonceMsgs := 0

	start := time.Now()
	vmValid := time.Duration(0)
	getbal := time.Duration(0)

	for _, msg := range mpool.Msgs {
		vmstart := time.Now()

		minGas := vm.PricelistByEpoch(ts.Height()).OnChainMessage(msg.ChainLength()) // TODO: really should be doing just msg.ChainLength() but the sync side of this code doesnt seem to have access to that
		if err := msg.VMMessage().ValidForBlockInclusion(minGas); err != nil {
			log.Infof("invalid message in message pool: %s", err)
			mpool.Remove(ctx, msg)
			continue
		}

		vmValid += time.Since(vmstart)

		// TODO: this should be in some more general 'validate message' call
		if msg.Message.GasLimit > build.BlockGasLimit {
			log.Warnf("message in mempool had too high of a gas limit (%d)", msg.Message.GasLimit)
			mpool.Remove(ctx, msg)
			continue
		}

		if msg.Message.To == address.Undef {
			log.Warnf("message in mempool had bad 'To' address")
			mpool.Remove(ctx, msg)
			continue
		}

		from := msg.Message.From

		getBalStart := time.Now()
		if _, ok := inclNonces[from]; !ok {
			act, err := al(ctx, from, ts.Key())
			if err != nil {
				log.Warnf("failed to check message sender balance, skipping message: %+v", err)
				mpool.Remove(ctx, msg)
				continue
			}

			inclNonces[from] = act.Nonce
			inclBalances[from] = act.Balance
		}
		getbal += time.Since(getBalStart)

		if inclBalances[from].LessThan(msg.Message.RequiredFunds()) {
			tooLowFundMsgs++
			// todo: drop from mpool
			mpool.Remove(ctx, msg)
			continue
		}

		if msg.Message.Nonce > inclNonces[from] {
			tooHighNonceMsgs++
			mpool.Remove(ctx, msg)
			continue
		}

		if msg.Message.Nonce < inclNonces[from] {
			//log.Warnf("message in mempool has already used nonce (%d < %d), %s (%d pending for)", msg.Message.Nonce, inclNonces[from], msg.Message.String(), countFrom(mpool.Msgs, from))
			mpool.Remove(ctx, msg)
			continue
		}

		inclNonces[from] = msg.Message.Nonce + 1
		inclBalances[from] = types.BigSub(inclBalances[from], msg.Message.RequiredFunds())
		inclCount[from]++

		out = append(out, msg)
		if len(out) >= build.BlockMessageLimit {
			break
		}
	}

	if tooLowFundMsgs > 0 {
		log.Warnf("%d messages in mempool does not have enough funds", tooLowFundMsgs)
	}

	if tooHighNonceMsgs > 0 {
		log.Warnf("%d messages in mempool had too high nonce", tooHighNonceMsgs)
	}

	sm := time.Now()
	if sm.Sub(start) > time.Second {
		log.Warnw("SelectMessages took a long time",
			"duration", sm.Sub(start),
			"vmvalidate", vmValid,
			"getbalance", getbal,
			"msgs", len(mpool.Msgs))
	}

	return out, nil
}
