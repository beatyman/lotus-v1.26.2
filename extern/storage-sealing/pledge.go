package sealing

import (
	"sync"
	"time"

	"github.com/filecoin-project/go-address"

	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/gwaylib/errors"
)

func NewSealPiece(maddr address.Address, sealer *ffiwrapper.Sealer) *Sealing {
	s := &Sealing{
		maddr:  maddr,
		sealer: sectorstorage.NewWorkerManager(sealer),
	}
	return s
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

	sb := m.sealer.(*sectorstorage.Manager).StorageProver.(*ffiwrapper.Sealer)

	// if task has consumed, auto do the next pledge.
	sb.SetAddPieceListener(func(t ffiwrapper.WorkerTask) {
		// success consume
		m.addConsumeTask()
	})
	m.addConsumeTask() // for init

	gcTimer := time.NewTicker(10 * time.Minute)

	go func() {
		defer func() {
			pledgeRunning = false
			sb.SetAddPieceListener(nil)
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
				//log.Info("GC CurWork")
				//dropTasks, err := sb.GcWorker(now.Add(-72 * time.Hour))
				//if err != nil {
				//	log.Error(errors.As(err))
				//} else {
				//	log.Infof("GC CurWork Done, drop:%+v", dropTasks)
				//}

				// just replenish
				m.addConsumeTask()
			case <-taskConsumed:
				stats := sb.GetAddPieceWait()
				// not accurate, if missing the taskConsumed event, it should replenish in gcTime.
				if stats > 0 {
					continue
				}
				go func() {
					// daemon check
					if err := m.PledgeRemoteSector(); err != nil {
						if errors.ErrNoData.Equal(err) {
							log.Error("No storage to allocate")
						} else {
							log.Errorf("%+v", err)
						}

						// if err happend, need to control the times.
						time.Sleep(10e9)

						// fast to do generate one addpiece event.
						m.addConsumeTask()
					}
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
	return nil
}
