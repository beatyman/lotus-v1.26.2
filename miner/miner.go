package miner

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	lru "github.com/hashicorp/golang-lru"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/types"

	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"
)

var log = logging.Logger("miner")

type waitFunc func(ctx context.Context, baseTime uint64) error

func NewMiner(api api.FullNode, epp gen.ElectionPoStProver) *Miner {
	arc, err := lru.NewARC(10000)
	if err != nil {
		panic(err)
	}

	return &Miner{
		api: api,
		epp: epp,
		waitFunc: func(ctx context.Context, baseTime uint64) error {
			// Wait around for half the block time in case other parents come in
			deadline := baseTime + build.PropagationDelay
			time.Sleep(time.Until(time.Unix(int64(deadline), 0)))

			return nil
		},
		minedBlockHeights: arc,
	}
}

type Miner struct {
	api api.FullNode

	epp gen.ElectionPoStProver

	lk        sync.Mutex
	addresses []address.Address
	stop      chan struct{}
	stopping  chan struct{}

	waitFunc waitFunc

	lastWork *MiningBase

	minedBlockHeights *lru.ARCCache
}

func (m *Miner) Addresses() ([]address.Address, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	out := make([]address.Address, len(m.addresses))
	copy(out, m.addresses)

	return out, nil
}

func (m *Miner) Register(addr address.Address) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	log.Infof("Register Miner:%s", addr)

	if len(m.addresses) > 0 {
		for _, a := range m.addresses {
			if fmt.Sprintf("%s", a) == fmt.Sprintf("%s", addr) {
				log.Warnf("miner.Register called more than once for actor '%s'", addr)
				return xerrors.Errorf("miner.Register called more than once for actor '%s'", addr)
			}
		}
	}

	m.addresses = append(m.addresses, addr)
	if len(m.addresses) == 1 {
		m.stop = make(chan struct{})
		go m.mine(context.TODO())
	}

	return nil
}

func (m *Miner) Unregister(ctx context.Context, addr address.Address) error {
	m.lk.Lock()
	defer m.lk.Unlock()
	if len(m.addresses) == 0 {
		return xerrors.New("no addresses registered")
	}

	idx := -1

	for i, a := range m.addresses {
		if fmt.Sprintf("%s", a) == fmt.Sprintf("%s", addr) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return xerrors.New("unregister: address not found")
	}

	m.addresses[idx] = m.addresses[len(m.addresses)-1]
	m.addresses = m.addresses[:len(m.addresses)-1]

	// Unregistering last address, stop mining first
	if len(m.addresses) == 0 && m.stop != nil {
		m.stopping = make(chan struct{})
		stopping := m.stopping
		close(m.stop)

		select {
		case <-stopping:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func nextRoundTime(base *MiningBase) time.Time {
	return time.Unix(int64(base.ts.MinTimestamp())+int64(build.BlockDelay*(base.nullRounds+1)), 0)
}
func (m *Miner) mine(ctx context.Context) {
	ctx, span := trace.StartSpan(ctx, "/mine")
	defer span.End()

	var lastBase MiningBase
	var nextRound time.Time
	const tryRounds = 1

eventLoop:
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

		m.lk.Lock()
		addrs := m.addresses
		m.lk.Unlock()

		prebase, err := m.GetBestMiningCandidate(ctx)
		if err != nil {
			log.Errorf("failed to get best mining candidate: %s", err)
			time.Sleep(time.Second * 5)
			continue
		}

		// Wait until propagation delay period after block we plan to mine on
		if err := m.waitFunc(ctx, prebase.ts.MinTimestamp()); err != nil {
			log.Error(err)
			return
		}

		base, err := m.GetBestMiningCandidate(ctx)
		if err != nil {
			log.Errorf("failed to get best mining candidate: %s", err)
			continue
		}
		now := time.Now()
		if !base.ts.Equals(lastBase.ts) {
			nextRound = nextRoundTime(base)
			lastBase = *base
		} else {
			log.Infof("BestMiningCandidate from the previous round: %s (nulls:%d)", lastBase.ts.Cids(), lastBase.nullRounds)

			// if the base was dead, make the nullRound++ step by round actually change.
			if (now.Unix()-nextRound.Unix())/build.BlockDelay == 0 {
				time.Sleep(1e9)
				continue
			}
			lastBase.nullRounds++
			nextRound = nextRoundTime(&lastBase)
		}
		subNextRoundTime := nextRound.Unix() - now.Unix()
		if subNextRoundTime > 0 && subNextRoundTime < (build.BlockDelay-build.PropagationDelay-build.PropagationDelay/2) {
			// no time to mining, just skip this round, and have to prepare to mine the next round.
			time.Sleep(nextRound.Sub(now))
			continue
		}

		blks := make([]*types.BlockMsg, 0)

		// just premine the next round
		for i, addr := range addrs {
			log.Infof("mineOne addrs:%d,%s", i, addr)
			b, err := m.mineOne(ctx, addr, base)
			if err != nil {
				log.Errorf("mining block failed: %+v", err)
				continue
			}
			if b != nil {
				blks = append(blks, b)
			}
		}

		if len(blks) != 0 {
			btime := time.Unix(int64(blks[0].Header.Timestamp), 0)
			if time.Now().Before(btime) {
				time.Sleep(time.Until(btime))
			} else {
				log.Warnw("mined block in the past", "block-time", btime,
					"time", time.Now(), "duration", time.Since(btime))
			}

			mWon := make(map[address.Address]struct{})
			for _, b := range blks {
				_, notOk := mWon[b.Header.Miner]
				if notOk {
					log.Errorw("2 blocks for the same miner. Throwing hands in the air. Report this. It is important.", "bloks", blks)
					continue eventLoop
				}
				mWon[b.Header.Miner] = struct{}{}
			}
			for _, b := range blks {
				// TODO: this code was written to handle creating blocks for multiple miners.
				// However, we don't use that, and we probably never will. So even though this code will
				// never see different miners, i'm going to handle the caching as if it was going to.
				// We can clean it up later when we remove all the multiple miner logic.
				blkKey := fmt.Sprintf("%s-%d", b.Header.Miner, b.Header.Height)
				if _, ok := m.minedBlockHeights.Get(blkKey); ok {
					log.Warnw("Created a block at the same height as another block we've created", "height", b.Header.Height, "miner", b.Header.Miner, "parents", b.Header.Parents)
					// continue
				}

				m.minedBlockHeights.Add(blkKey, true)
				if err := m.api.SyncSubmitBlock(ctx, b); err != nil {
					log.Errorf("failed to submit newly mined block: %s", err)
				}
			}
		} else {
			now := time.Now()
			if nextRound.Before(now) {
				nextRound = now.Add(time.Duration(build.BlockDelay-(now.Unix()-nextRound.Unix())%build.BlockDelay) * time.Second)
			}
			// goto next round
			log.Info("mine next round at:", nextRound.Format(time.RFC3339))
			select {
			case <-time.After(nextRound.Sub(now)):
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
	ts         *types.TipSet
	nullRounds abi.ChainEpoch
}

func (m *Miner) GetBestMiningCandidate(ctx context.Context) (*MiningBase, error) {
	bts, err := m.api.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	if m.lastWork != nil {
		if m.lastWork.ts.Equals(bts) {
			return m.lastWork, nil
		}

		btsw, err := m.api.ChainTipSetWeight(ctx, bts.Key())
		if err != nil {
			return nil, err
		}
		ltsw, err := m.api.ChainTipSetWeight(ctx, m.lastWork.ts.Key())
		if err != nil {
			return nil, err
		}

		if types.BigCmp(btsw, ltsw) <= 0 {
			return m.lastWork, nil
		}
	}

	m.lastWork = &MiningBase{ts: bts}
	return m.lastWork, nil
}

func (m *Miner) hasPower(ctx context.Context, addr address.Address, ts *types.TipSet) (bool, error) {
	power, err := m.api.StateMinerPower(ctx, addr, ts.Key())
	if err != nil {
		return false, err
	}

	return !power.MinerPower.Equals(types.NewInt(0)), nil
}

func (m *Miner) mineOne(ctx context.Context, addr address.Address, base *MiningBase) (*types.BlockMsg, error) {
	log.Debugw("attempting to mine a block", "tipset", types.LogCids(base.ts.Cids()))
	start := time.Now()

	hasPower, err := m.hasPower(ctx, addr, base.ts)
	if err != nil {
		return nil, xerrors.Errorf("checking if miner is slashed: %w", err)
	}
	if !hasPower {
		// slashed or just have no power yet
		base.nullRounds++
		return nil, nil
	}
	log.Info("get pending message")
	// make auto clean for pending messages every round.
	pending, err := m.api.MpoolPending(context.TODO(), base.ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get pending messages: %w", err)
	}
	log.Infof("all pending msgs len:%d", len(pending))
	pending, err = SelectMessages(context.TODO(), m.api.StateGetActor, base.ts, &MsgPool{FromApi: m.api, Msgs: pending})
	if err != nil {
		return nil, xerrors.Errorf("message filtering failed: %w", err)
	}
	if len(pending) > build.BlockMessageLimit {
		pending = pending[:build.BlockMessageLimit]
	}

	log.Infof("Time delta between now and our mining base: %ds (nulls: %d)", uint64(time.Now().Unix())-base.ts.MinTimestamp(), base.nullRounds)

	proofin, err := gen.IsRoundWinner(ctx, base.ts, int64(base.ts.Height()+base.nullRounds+1), addr, m.epp, m.api)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if we win next round: %w", err)
	}

	if proofin == nil {
		log.Info("Not Win")
		base.nullRounds++
		return nil, nil
	}

	proof, err := gen.ComputeProof(ctx, m.epp, proofin)
	if err != nil {
		return nil, xerrors.Errorf("computing election proof: %w", err)
	}

	ticket, err := m.computeTicket(ctx, addr, base)
	if err != nil {
		return nil, xerrors.Errorf("scratching ticket failed: %w", err)
	}

	b, err := m.createBlock(base, addr, ticket, proof, pending)
	if err != nil {
		return nil, xerrors.Errorf("failed to create block: %w", err)
	}

	dur := time.Since(start)
	log.Infow("mined new block",
		"cid", b.Cid(),
		"height", b.Header.Height,
		"weight", b.Header.ParentWeight,
		"rounds", base.nullRounds,
		"took", dur,
		"submit", time.Unix(int64(base.ts.MinTimestamp()+(uint64(base.nullRounds)+1)*build.BlockDelay), 0).Format(time.RFC3339))
	if dur > time.Second*build.BlockDelay {
		log.Warn("CAUTION: block production took longer than the block delay. Your computer may not be fast enough to keep up")
	}

	return b, nil
}

func (m *Miner) computeTicket(ctx context.Context, addr address.Address, base *MiningBase) (*types.Ticket, error) {
	w, err := m.api.StateMinerWorker(ctx, addr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := addr.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	input, err := m.api.ChainGetRandomness(ctx, base.ts.Key(), crypto.DomainSeparationTag_TicketProduction, (base.ts.Height()+base.nullRounds+1)-1, buf.Bytes())
	if err != nil {
		return nil, err
	}

	vrfOut, err := gen.ComputeVRF(ctx, m.api.WalletSign, w, input)
	if err != nil {
		return nil, err
	}

	return &types.Ticket{
		VRFProof: vrfOut,
	}, nil
}

func (m *Miner) createBlock(base *MiningBase, addr address.Address, ticket *types.Ticket, proof *types.EPostProof, msgs []*types.SignedMessage) (*types.BlockMsg, error) {
	uts := base.ts.MinTimestamp() + uint64(build.BlockDelay*(base.nullRounds+1))

	nheight := base.ts.Height() + base.nullRounds + 1

	// why even return this? that api call could just submit it for us
	log.Infof("MinerCreateBlock validated pending msgs len:%d", len(msgs))
	return m.api.MinerCreateBlock(context.TODO(), addr, base.ts.Key(), ticket, proof, msgs, nheight, uint64(uts))
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
	// if err := p.FromApi.MpoolRemove(ctx, msg.Message.From, msg.Message.Nonce); err != nil {
	// 	log.Warn(errors.As(err))
	// }
}

func SelectMessages(ctx context.Context, al ActorLookup, ts *types.TipSet, mpool *MsgPool) ([]*types.SignedMessage, error) {
	out := make([]*types.SignedMessage, 0, build.BlockMessageLimit)
	inclNonces := make(map[address.Address]uint64)
	inclBalances := make(map[address.Address]types.BigInt)
	inclCount := make(map[address.Address]int)

	// TODO: too more log from here, and waiting upgrade from offical.
	for _, msg := range mpool.Msgs {

		if msg.Message.To == address.Undef {
			log.Warnf("message in mempool had bad 'To' address")
			mpool.Remove(ctx, msg)
			continue
		}

		from := msg.Message.From

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

		if inclBalances[from].LessThan(msg.Message.RequiredFunds()) {
			log.Warnf("message in mempool does not have enough funds: %s", msg.Message.String())
			mpool.Remove(ctx, msg)
			continue
		}

		if msg.Message.Nonce > inclNonces[from] {
			log.Warnf("message in mempool has too high of a nonce (%d > %d, inclcount %d) %s (%d pending for orig)", msg.Message.Nonce, inclNonces[from], inclCount[from], msg.Message.String(), countFrom(mpool.Msgs, from))
			mpool.Remove(ctx, msg)
			continue
		}

		if msg.Message.Nonce < inclNonces[from] {
			log.Warnf("message in mempool has already used nonce (%d < %d), %s (%d pending for)", msg.Message.Nonce, inclNonces[from], msg.Message.String(), countFrom(mpool.Msgs, from))
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
	return out, nil
}
