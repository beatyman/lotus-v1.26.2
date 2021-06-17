package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gwaylib/errors"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/specs-storage/storage"

	fapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/node/config"

	"go.opencensus.io/trace"
)

type WindowPoStScheduler struct {
	api              storageMinerApi
	feeCfg           config.MinerFeeConfig
	addrSel          *AddressSelector
	prover           storage.Prover
	verifier         ffiwrapper.Verifier
	faultTracker     sectorstorage.FaultTracker
	proofType        abi.RegisteredPoStProof
	partitionSectors uint64
	ch               *changeHandler

	actor address.Address

	evtTypes [4]journal.EventType
	journal  journal.Journal

	// failed abi.ChainEpoch // eps
	// failLk sync.Mutex

	autoWithdrawLk        sync.Mutex
	autoWithdrawLastEpoch int64
	autoWithdrawRunning   bool

	wdpostLogsLk sync.Mutex
	wdpostLogs   map[uint64][]fapi.WdPoStLog // log the process of wdpost, dealine:logs
}

func NewWindowedPoStScheduler(api storageMinerApi, fc config.MinerFeeConfig, as *AddressSelector, sb storage.Prover, verif ffiwrapper.Verifier, ft sectorstorage.FaultTracker, j journal.Journal, actor address.Address) (*WindowPoStScheduler, error) {
	log.Info("lookup default config: EnableSeparatePartition::", fc.EnableSeparatePartition, "PartitionsPerMsg::", fc.PartitionsPerMsg)
	EnableSeparatePartition = fc.EnableSeparatePartition
	if EnableSeparatePartition && fc.PartitionsPerMsg != 0 {
		PartitionsPerMsg = fc.PartitionsPerMsg
	}
	mi, err := api.StateMinerInfo(context.TODO(), actor, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	return &WindowPoStScheduler{
		api:              api,
		feeCfg:           fc,
		addrSel:          as,
		prover:           sb,
		verifier:         verif,
		faultTracker:     ft,
		proofType:        mi.WindowPoStProofType,
		partitionSectors: mi.WindowPoStPartitionSectors,

		actor: actor,
		evtTypes: [...]journal.EventType{
			evtTypeWdPoStScheduler:  j.RegisterEventType("wdpost", "scheduler"),
			evtTypeWdPoStProofs:     j.RegisterEventType("wdpost", "proofs_processed"),
			evtTypeWdPoStRecoveries: j.RegisterEventType("wdpost", "recoveries_processed"),
			evtTypeWdPoStFaults:     j.RegisterEventType("wdpost", "faults_processed"),
		},
		journal: j,

		wdpostLogs: map[uint64][]fapi.WdPoStLog{},
	}, nil
}

func (s *WindowPoStScheduler) ResetLog(index uint64) {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	delete(s.wdpostLogs, index)
}
func (s *WindowPoStScheduler) GetLog(index uint64) []fapi.WdPoStLog {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	l, _ := s.wdpostLogs[index]
	return l
}
func (s *WindowPoStScheduler) PutLog(index uint64, format string, args ...interface{}) string {
	s.wdpostLogsLk.Lock()
	defer s.wdpostLogsLk.Unlock()
	output := fapi.WdPoStLog{Time: time.Now(), Log: fmt.Sprintf(format, args...)}
	l, ok := s.wdpostLogs[index]
	if !ok {
		l = []fapi.WdPoStLog{output}
	} else {
		l = append(l, output)
	}
	s.wdpostLogs[index] = l
	return output.Log
}

type changeHandlerAPIImpl struct {
	storageMinerApi
	*WindowPoStScheduler
}

func nextRoundTime(ts *types.TipSet) time.Time {
	return time.Unix(int64(ts.MinTimestamp())+int64(build.BlockDelaySecs)+int64(build.PropagationDelaySecs), 0)
}

func (s *WindowPoStScheduler) Run(ctx context.Context) {
	// Initialize change handler
	chImpl := &changeHandlerAPIImpl{storageMinerApi: s.api, WindowPoStScheduler: s}
	s.ch = newChangeHandler(chImpl, s.actor)
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
			return
		}
	}

	// close this function and use the timer from mining
	return
	// end by hlm

	var notifs <-chan []*fapi.HeadChange
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
