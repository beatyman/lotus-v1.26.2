package sealing

import (
	"context"
	"sync"
	"time"

	"github.com/filecoin-project/sector-storage/database"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/gwaylib/errors"
)

type Pledge struct {
	SectorID abi.SectorID
	Sealing  *Sealing

	SectorBuilder *ffiwrapper.Sealer

	ActAddr    address.Address
	WorkerAddr address.Address

	ExistingPieceSizes []abi.UnpaddedPieceSize
	Sizes              []abi.UnpaddedPieceSize
}

// Export the garbage.go#sealing.pledgeSector, so they should have same logic.
func (g *Pledge) PledgeSector(ctx context.Context) ([]ffiwrapper.Piece, error) {
	sectorID := g.SectorID
	sizes := g.Sizes
	existingPieceSizes := g.ExistingPieceSizes
	log.Infof("DEBUG:PledgeSector in, %d,%d", sectorID, len(sizes))
	defer log.Infof("DEBUG:PledgeSector out, %d", sectorID)

	if len(sizes) == 0 {
		log.Infof("DEBGUG:PledgeSector no sizes, %d", sectorID)
		return nil, nil
	}

	log.Infof("Pledge %d, contains %+v", sectorID, existingPieceSizes)

	out := make([]ffiwrapper.Piece, len(sizes))
	for i, size := range sizes {
		ppi, err := g.Sealing.sealer.AddPiece(ctx, sectorID, existingPieceSizes, size, g.Sealing.pledgeReader(size))
		if err != nil {
			return nil, xerrors.Errorf("add piece: %w", err)
		}

		g.ExistingPieceSizes = append(g.ExistingPieceSizes, size)
		out[i] = ffiwrapper.Piece{
			Size:  ppi.Size.Unpadded(),
			CommP: ppi.PieceCID,
		}
	}

	return out, nil
}

// export Sealing.PledgeSector for remote worker for calling.
func (m *Sealing) PledgeRemoteSector() error {
	ctx := context.TODO() // we can't use the context from command which invokes
	// this, as we run everything here async, and it's cancelled when the
	// command exits

	size := abi.PaddedPieceSize(m.sealer.SectorSize()).Unpadded()

	_, rt, err := ffiwrapper.ProofTypeFromSectorSize(m.sealer.SectorSize())
	if err != nil {
		return errors.As(err)
	}

	sid, err := m.sc.Next()
	if err != nil {
		return errors.As(err)
	}
	sectorID := m.minerSector(sid)
	if err := m.sealer.NewSector(ctx, sectorID); err != nil {
		return errors.As(err)
	}

	pieces, err := m.sealer.(*ffiwrapper.Sealer).PledgeSector(sectorID, []abi.UnpaddedPieceSize{size})
	if err != nil {
		return errors.As(err)
	}

	if err := m.newSector(sid, rt, pieces); err != nil {
		return errors.As(err)
	}
	return nil
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

	sb := m.sealer.(*ffiwrapper.Sealer)

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
			case now := <-gcTimer.C:
				log.Info("GC CurWork")
				if err := database.GcCurWork(now.Add(-48 * time.Hour)); err != nil {
					log.Error(errors.As(err))
				}
				log.Info("GC CurWork Done")
				// just replenish
				m.addConsumeTask()
			case <-taskConsumed:
				stats := sb.WorkerStats()
				// not accurate, if missing the taskConsumed event, it should replenish in gcTime.
				if stats.AddPieceWait > 0 {
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
