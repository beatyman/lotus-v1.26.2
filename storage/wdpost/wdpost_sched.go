package wdpost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v8/miner"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/storage/ctladdr"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
)

var log = logging.Logger("wdpost")

type NodeAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	ChainNotify(context.Context) (<-chan []*api.HeadChange, error)

	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	StateMinerSectors(context.Context, address.Address, *bitfield.BitField, types.TipSetKey) ([]*miner.SectorOnChainInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	StateGetRandomnessFromTickets(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	StateGetRandomnessFromBeacon(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	StateWaitMsg(ctx context.Context, cid cid.Cid, confidence uint64, limit abi.ChainEpoch, allowReplaced bool) (*api.MsgLookup, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]api.Partition, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)

	MpoolPushMessage(context.Context, *types.Message, *api.MessageSendSpec) (*types.SignedMessage, error)

	GasEstimateMessageGas(context.Context, *types.Message, *api.MessageSendSpec, types.TipSetKey) (*types.Message, error)
	GasEstimateFeeCap(context.Context, *types.Message, int64, types.TipSetKey) (types.BigInt, error)
	GasEstimateGasPremium(_ context.Context, nblocksincl uint64, sender address.Address, gaslimit int64, tsk types.TipSetKey) (types.BigInt, error)

	WalletBalance(context.Context, address.Address) (types.BigInt, error)
	WalletHas(context.Context, address.Address) (bool, error)
	//hlm
	ChainGetTipSetByHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	MpoolSignMessage(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec) (*types.SignedMessage, error) // only sign the message in mpool
	ChainComputeBaseFee(context.Context, types.TipSetKey) (types.BigInt, error)
	MpoolPush(context.Context, *types.SignedMessage) (cid.Cid, error) //perm:write
	WalletSignMessage(context.Context, address.Address, *types.Message) (*types.SignedMessage, error) //perm:sign
	StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (types.BigInt, error) //perm:read
}

// WindowPoStScheduler is the coordinator for WindowPoSt submissions, fault
// declaration, and recovery declarations. It watches the chain for reverts and
// applies, and schedules/run those processes as partition deadlines arrive.
//
// WindowPoStScheduler watches the chain though the changeHandler, which in turn
// turn calls the scheduler when the time arrives to do work.
type WindowPoStScheduler struct {
	api                             NodeAPI
	feeCfg                          config.MinerFeeConfig
	addrSel                         *ctladdr.AddressSelector
	prover                          storiface.ProverPoSt
	verifier                        storiface.Verifier
	faultTracker                    sealer.FaultTracker
	proofType                       abi.RegisteredPoStProof
	partitionSectors                uint64
	disablePreChecks                bool
	maxPartitionsPerPostMessage     int
	maxPartitionsPerRecoveryMessage int
	ch                              *changeHandler

	actor address.Address

	evtTypes [4]journal.EventType
	journal  journal.Journal

	// failed abi.ChainEpoch // eps
	// failLk sync.Mutex

	autoWithdrawLk        sync.Mutex
	autoWithdrawLastEpoch int64
	autoWithdrawRunning   bool

	wdpostLogsLk sync.Mutex
	wdpostLogs   map[uint64][]api.WdPoStLog // log the process of wdpost, dealine:logs
}

// NewWindowedPoStScheduler creates a new WindowPoStScheduler scheduler.
func NewWindowedPoStScheduler(fapi NodeAPI,
	cfg config.MinerFeeConfig,
	pcfg config.ProvingConfig,
	as *ctladdr.AddressSelector,
	sp storiface.ProverPoSt,
	verif storiface.Verifier,
	ft sealer.FaultTracker,
	j journal.Journal,
	actor address.Address) (*WindowPoStScheduler, error) {
	log.Info("lookup default config: EnableSeparatePartition::", cfg.EnableSeparatePartition, "PartitionsPerMsg::", cfg.PartitionsPerMsg)
	EnableSeparatePartition = cfg.EnableSeparatePartition
	if EnableSeparatePartition && cfg.PartitionsPerMsg != 0 {
		PartitionsPerMsg = cfg.PartitionsPerMsg
	}
	mi, err := fapi.StateMinerInfo(context.TODO(), actor, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	return &WindowPoStScheduler{
		api:                             fapi,
		feeCfg:                          cfg,
		addrSel:                         as,
		prover:                          sp,
		verifier:                        verif,
		faultTracker:                    ft,
		proofType:                       mi.WindowPoStProofType,
		partitionSectors:                mi.WindowPoStPartitionSectors,
		disablePreChecks:                pcfg.DisableWDPoStPreChecks,
		maxPartitionsPerPostMessage:     pcfg.MaxPartitionsPerPoStMessage,
		maxPartitionsPerRecoveryMessage: pcfg.MaxPartitionsPerRecoveryMessage,
		actor:                           actor,
		evtTypes: [...]journal.EventType{
			evtTypeWdPoStScheduler:  j.RegisterEventType("wdpost", "scheduler"),
			evtTypeWdPoStProofs:     j.RegisterEventType("wdpost", "proofs_processed"),
			evtTypeWdPoStRecoveries: j.RegisterEventType("wdpost", "recoveries_processed"),
			evtTypeWdPoStFaults:     j.RegisterEventType("wdpost", "faults_processed"),
		},
		journal: j,

		wdpostLogs: map[uint64][]api.WdPoStLog{},
	}, nil
}

func (s *WindowPoStScheduler) ResetLog(index uint64) {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	delete(s.wdpostLogs, index)
}
func (s *WindowPoStScheduler) GetLog(index uint64) []api.WdPoStLog {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	l, _ := s.wdpostLogs[index]
	return l
}
func (s *WindowPoStScheduler) PutLogw(index uint64, args ...interface{}) string {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	src := []byte(fmt.Sprintln(args...))
	output := api.WdPoStLog{Time: time.Now(), Log: string(src[:len(src)-1])}
	l, ok := s.wdpostLogs[index]
	if !ok {
		l = []api.WdPoStLog{output}
	} else {
		l = append(l, output)
	}
	s.wdpostLogs[index] = l
	return output.Log
}
func (s *WindowPoStScheduler) PutLogf(index uint64, format string, args ...interface{}) string {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	output := api.WdPoStLog{Time: time.Now(), Log: fmt.Sprintf(format, args...)}
	l, ok := s.wdpostLogs[index]
	if !ok {
		l = []api.WdPoStLog{output}
	} else {
		l = append(l, output)
	}
	s.wdpostLogs[index] = l
	return output.Log
}


func nextRoundTime(ts *types.TipSet) time.Time {
	return time.Unix(int64(ts.MinTimestamp())+int64(build.BlockDelaySecs)+int64(build.PropagationDelaySecs), 0)
}

func (s *WindowPoStScheduler) Run(ctx context.Context) {
	// Initialize change handler.

	// callbacks is a union of the fullNodeFilteredAPI and ourselves.
	callbacks := struct {
		NodeAPI
		*WindowPoStScheduler
	}{s.api, s}

	s.ch = newChangeHandler(callbacks, s.actor)
	defer s.ch.shutdown()
	s.ch.start()

	// implement by hlm
	var lastTsHeight abi.ChainEpoch
	for {
		bts, err := s.api.ChainHead(ctx)
		if err != nil {
			log.Error(errors.As(err))
			time.Sleep(time.Second)
			continue
		}
		if bts.Height() == lastTsHeight {
			time.Sleep(time.Second)
			continue
		}

		log.Infof("Checking window post at:%d", bts.Height())
		lastTsHeight = bts.Height()
		s.update(ctx, nil, bts)

		// loop to next time.
		select {
		case <-time.After(time.Until(nextRoundTime(bts))):
			continue
		case <-ctx.Done():
			log.Info("wdpost ctx exit")
			return
		}
	}

	// close this function and use the timer from mining
	return
	// end by hlm

	var notifs <-chan []*api.HeadChange
	var err error
	var gotCur bool

	// not fine to panic after this point
	for {
		if notifs == nil {
			notifs, err = s.api.ChainNotify(ctx)
			if err != nil {
				log.Errorf("ChainNotify error: %+v", err)

				build.Clock.Sleep(10 * time.Second)
				continue
			}

			gotCur = false
			log.Info("restarting window post scheduler")
		}

		select {
		case changes, ok := <-notifs:
			if !ok {
				log.Warn("window post scheduler notifs channel closed")
				notifs = nil
				continue
			}

			if !gotCur {
				if len(changes) != 1 {
					log.Errorf("expected first notif to have len = 1")
					continue
				}
				chg := changes[0]
				if chg.Type != store.HCCurrent {
					log.Errorf("expected first notif to tell current ts")
					continue
				}

				ctx, span := trace.StartSpan(ctx, "WindowPoStScheduler.headChange")

				s.update(ctx, nil, chg.Val)

				span.End()
				gotCur = true
				continue
			}

			ctx, span := trace.StartSpan(ctx, "WindowPoStScheduler.headChange")

			var lowest, highest *types.TipSet = nil, nil

			for _, change := range changes {
				if change.Val == nil {
					log.Errorf("change.Val was nil")
				}
				switch change.Type {
				case store.HCRevert:
					lowest = change.Val
				case store.HCApply:
					highest = change.Val
				}
			}

			s.update(ctx, lowest, highest)

			span.End()
		case <-ctx.Done():
			return
		}
	}
}

func (s *WindowPoStScheduler) update(ctx context.Context, revert, apply *types.TipSet) {
	if apply == nil {
		log.Error("no new tipset in window post WindowPoStScheduler.update")
		return
	}

	s.autoWithdraw(apply) // by hlm

	err := s.ch.update(ctx, revert, apply)
	if err != nil {
		log.Errorf("handling head updates in window post sched: %+v", err)
	}
}

// onAbort is called when generating proofs or submitting proofs is aborted
func (s *WindowPoStScheduler) onAbort(ts *types.TipSet, deadline *dline.Info) {
	s.journal.RecordEvent(s.evtTypes[evtTypeWdPoStScheduler], func() interface{} {
		c := evtCommon{}
		if ts != nil {
			c.Deadline = deadline
			c.Height = ts.Height()
			c.TipSet = ts.Cids()
		}
		return WdPoStSchedulerEvt{
			evtCommon: c,
			State:     SchedulerStateAborted,
		}
	})
}

func (s *WindowPoStScheduler) getEvtCommon(err error) evtCommon {
	c := evtCommon{Error: err}
	currentTS, currentDeadline := s.ch.currentTSDI()
	if currentTS != nil {
		c.Deadline = currentDeadline
		c.Height = currentTS.Height()
		c.TipSet = currentTS.Cids()
	}
	return c
}
