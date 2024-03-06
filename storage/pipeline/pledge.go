package sealing

import (
	"context"
	"sync"
	"time"
	sectorstorage "github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/gwaylib/errors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/types"
)

var initialPledgeNum = types.NewInt(110)
var initialPledgeDen = types.NewInt(100)

func (m *Sealing) pledgeForPower(ctx context.Context, addedPower abi.StoragePower) (abi.TokenAmount, error) {
	store := adt.WrapStore(ctx, cbor.NewCborStore(bstore.NewAPIBlockstore(m.Api)))

	// load power actor
	var (
		powerSmoothed    builtin.FilterEstimate
		pledgeCollateral abi.TokenAmount
	)
	if act, err := m.Api.StateGetActor(ctx, power.Address, types.EmptyTSK); err != nil {
		return types.EmptyInt, xerrors.Errorf("loading power actor: %w", err)
	} else if s, err := power.Load(store, act); err != nil {
		return types.EmptyInt, xerrors.Errorf("loading power actor state: %w", err)
	} else if p, err := s.TotalPowerSmoothed(); err != nil {
		return types.EmptyInt, xerrors.Errorf("failed to determine total power: %w", err)
	} else if c, err := s.TotalLocked(); err != nil {
		return types.EmptyInt, xerrors.Errorf("failed to determine pledge collateral: %w", err)
	} else {
		powerSmoothed = p
		pledgeCollateral = c
	}

	// load reward actor
	rewardActor, err := m.Api.StateGetActor(ctx, reward.Address, types.EmptyTSK)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading reward actor: %w", err)
	}

	rewardState, err := reward.Load(store, rewardActor)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading reward actor state: %w", err)
	}

	// get circulating supply
	circSupply, err := m.Api.StateVMCirculatingSupplyInternal(ctx, types.EmptyTSK)
	if err != nil {
		return big.Zero(), xerrors.Errorf("getting circulating supply: %w", err)
	}

	// do the calculation
	initialPledge, err := rewardState.InitialPledgeForPower(
		addedPower,
		pledgeCollateral,
		&powerSmoothed,
		circSupply.FilCirculating,
	)
	if err != nil {
		return big.Zero(), xerrors.Errorf("calculating initial pledge: %w", err)
	}

	return types.BigDiv(types.BigMul(initialPledge, initialPledgeNum), initialPledgeDen), nil
}

func (m *Sealing) sectorWeight(ctx context.Context, sector SectorInfo, expiration abi.ChainEpoch) (abi.StoragePower, error) {
	spt, err := m.currentSealProof(ctx)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("getting seal proof type: %w", err)
	}

	ssize, err := spt.SectorSize()
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("getting sector size: %w", err)
	}

	ts, err := m.Api.ChainHead(ctx)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("getting chain head: %w", err)
	}

	// get verified deal infos
	var w, vw = big.Zero(), big.Zero()

	for _, piece := range sector.Pieces {
		if !piece.HasDealInfo() {
			// todo StateMinerInitialPledgeCollateral doesn't add cc/padding to non-verified weight, is that correct?
			continue
		}

		alloc, err := piece.GetAllocation(ctx, m.Api, ts.Key())
		if err != nil || alloc == nil {
			w = big.Add(w, abi.NewStoragePower(int64(piece.Piece().Size)))
			continue
		}

		vw = big.Add(vw, abi.NewStoragePower(int64(piece.Piece().Size)))
	}

	// load market actor
	duration := expiration - ts.Height()
	sectorWeight := builtin.QAPowerForWeight(ssize, duration, w, vw)

	return sectorWeight, nil
}


var (
	pledgeExit    = make(chan bool, 1)
	pledgeRunning = false
	pledgeSync    = sync.Mutex{}
)

var (
	taskConsumed = make(chan int, 1)
)

func (m *Sealing) addConsumeTask() {
	select {
	case taskConsumed <- 1:
	default:
		// ignore
	}
}

func (m *Sealing) RunPledgeSector() error {
	pledgeSync.Lock()
	defer pledgeSync.Unlock()
	if pledgeRunning {
		return errors.New("In running")
	}
	pledgeRunning = true
	log.Info("Pledge garbage start")

	sb := m.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)

	// if task has consumed, auto do the next pledge.
	sb.SetPledgeListener(func(t ffiwrapper.WorkerTask) {
		// success consume
		m.addConsumeTask()
	})
	m.addConsumeTask() // for init

	gcTimer := time.NewTicker(10 * time.Minute)

	go func() {
		defer func() {
			pledgeRunning = false
			sb.SetPledgeListener(nil)
			gcTimer.Stop()
			log.Info("Pledge daemon exited.")

			// auto recover for panic
			if err := recover(); err != nil {
				log.Error(errors.New("Pledge daemon not exit by normal, goto auto restart").As(err))
				m.RunPledgeSector()
			}
		}()
		for {
			pledgeRunning = true
			select {
			case <-pledgeExit:
				return
			case <-gcTimer.C:
				// close gc to manully control
				log.Info("GC CurWork")
				//dropTasks, err := sb.GcTimeoutTask(now.Add(-120 * time.Hour))
				//if err != nil {
				//	log.Error(errors.As(err))
				//} else {
				//	log.Infof("GC CurWork Done, drop:%+v", dropTasks)
				//}

				gcTasks, err := sb.GcWorker("")
				if err != nil {
					log.Warn(errors.As(err))
				}
				for _, task := range gcTasks {
					log.Infof("gc : %s\n", task)
				}
				log.Infof("gc done")

				// just replenish
				m.addConsumeTask()
			case <-taskConsumed:
				stats := sb.GetPledgeWait()
				// not accurate, if missing the taskConsumed event, it should replenish in gcTime.
				if stats > 0 {
					log.Infow("pledge loop continue", "wait-count", stats)
					continue
				}
				go func() {
					// daemon check
					sectorRef, err := m.PledgeSector(context.TODO())
					if err != nil {
						if errors.ErrNoData.Equal(err) {
							log.Error("No storage to allocate")
						} else {
							log.Errorf("%+v", err)
						}

						// if err happend, need to control the times.
						time.Sleep(10e9)

						// fast to do generate one addpiece event.
						m.addConsumeTask()
						return
					}

					// TODO:
					_ = sectorRef
				}()
			}
		}
	}()
	return nil
}

func (m *Sealing) StatusPledgeSector() (int, error) {
	pledgeSync.Lock()
	defer pledgeSync.Unlock()
	if !pledgeRunning {
		return 0, nil
	}
	return 1, nil
}

func (m *Sealing) ExitPledgeSector() error {
	pledgeSync.Lock()
	if !pledgeRunning {
		pledgeSync.Unlock()
		return errors.New("Not in running")
	}
	if len(pledgeExit) > 0 {
		pledgeSync.Unlock()
		return errors.New("Exiting")
	}
	pledgeSync.Unlock()

	pledgeExit <- true
	log.Info("Pledge garbage exit")
	return nil
}
