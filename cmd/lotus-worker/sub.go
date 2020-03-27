package main

import (
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/go-sectorbuilder/database"

	"github.com/filecoin-project/lotus/storage/sealing"
	"github.com/gwaylib/errors"
)

type worker struct {
	minerEndpoint string
	repo          string
	sealedRepo    string
	auth          http.Header

	actAddr    address.Address
	workerAddr address.Address

	sb        *sectorbuilder.SectorBuilder
	sealedSB  *sectorbuilder.SectorBuilder
	workerCfg sectorbuilder.WorkerCfg

	workMu sync.Mutex
	workOn map[string]sectorbuilder.WorkerTask // task key
}

func acceptJobs(ctx context.Context,
	sb, sealedSB *sectorbuilder.SectorBuilder,
	act, workerAddr address.Address,
	endpoint string, auth http.Header,
	repo, sealedRepo string,
	noaddpiece, noprecommit, nocommit bool) error {
	workerId := GetWorkerID(repo)
	netIp := os.Getenv("NETIP")

	workerCfg := sectorbuilder.WorkerCfg{
		ID: workerId,
		IP: netIp,
	}

	w := &worker{
		minerEndpoint: endpoint,
		auth:          auth,
		repo:          repo,
		sealedRepo:    sealedRepo,

		sb: sb,

		actAddr:    act,
		workerAddr: workerAddr,
		sealedSB:   sealedSB,
		workerCfg:  workerCfg,
		workOn:     map[string]sectorbuilder.WorkerTask{},
	}

	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	tasks, err := api.WorkerQueue(ctx, workerCfg)
	if err != nil {
		return err
	}
	log.Info("Worker started")

loop:
	for {
		// log.Infof("Waiting for new task")
		// checking is connection aliveable,if not, do reconnect.
		aliveChecking := time.After(1 * time.Minute) // waiting out
		select {
		case <-aliveChecking:
			ReleaseNodeApi(false)
			_, err := GetNodeApi()
			if err != nil {
				log.Warn(errors.As(err))
			}
		case task := <-tasks:
			log.Infof("New task: %s, sector %s, action: %d", task.Key(), task.GetSectorID(), task.Type)
			go func(task sectorbuilder.WorkerTask) {
				taskKey := task.Key()
				w.workMu.Lock()
				if _, ok := w.workOn[taskKey]; ok {
					w.workMu.Unlock()
					// when the miner restart, it should send the same task,
					// and this worker is already working on, so drop this job.
					log.Info("task(%s) is in working", taskKey)
					return
				} else {
					w.workOn[taskKey] = task
					w.workMu.Unlock()
				}

				defer func() {
					w.workMu.Lock()
					delete(w.workOn, taskKey)
					w.workMu.Unlock()
				}()

				res := w.processTask(ctx, task)

				log.Infof("Task %s done, err: %+v", task.Key(), res.GoErr)

				// retry to commit the result.
				w.workerDone(ctx, task, res)
			}(task)

		case <-ctx.Done():
			break loop
		}
	}

	log.Warn("acceptJobs exit")
	return nil
}

func (w *worker) addPiece(ctx context.Context, task sectorbuilder.WorkerTask) ([]sectorbuilder.Piece, error) {
	sizes := task.PieceSizes
	api, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}

	s := sealing.NewSealPiece(w.actAddr, w.workerAddr, w.sb)
	g := &sealing.Pledge{
		SectorNum:          task.SectorNum,
		Sealing:            s,
		Api:                api,
		SectorBuilder:      w.sb,
		ActAddr:            w.actAddr,
		WorkerAddr:         w.workerAddr,
		ExistingPieceSizes: []uint64{},
		Sizes:              sizes,
	}
	return g.PledgeSector(ctx)
}

func (w *worker) cleanCache(ctx context.Context) error {
	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	ws, err := api.WorkerWorking(ctx, w.workerCfg.ID)
	if err != nil {
		ReleaseNodeApi(false)
		return errors.As(err, w.workerCfg.IP)
	}

	sealed := filepath.Join(w.repo, "sealed")
	cache := filepath.Join(w.repo, "cache")
	staged := filepath.Join(w.repo, "staging")

	sealedFiles, err := ioutil.ReadDir(sealed)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
	sealedLoop:
		for _, f := range sealedFiles {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue sealedLoop
				}
			}
			if err := os.RemoveAll(filepath.Join(sealed, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(sealed, f.Name()))
			}
		}
	}

	cacheFiles, err := ioutil.ReadDir(cache)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
	cacheLoop:
		for _, f := range cacheFiles {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue cacheLoop
				}
			}
			if err := os.RemoveAll(filepath.Join(cache, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(cache, f.Name()))
			}
		}
	}
	stagedFiles, err := ioutil.ReadDir(staged)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
	stagedLoop:
		for _, f := range stagedFiles {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue stagedLoop
				}
			}
			if err := os.RemoveAll(filepath.Join(staged, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(staged, f.Name()))
			}
		}
	}

	if err := os.MkdirAll(sealed, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	if err := os.MkdirAll(cache, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	if err := os.MkdirAll(staged, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	return nil
}

func (w *worker) pushCache(ctx context.Context, task sectorbuilder.WorkerTask) error {
	log.Infof("pushCache:%+v", task.GetSectorID())
	defer log.Infof("pushCache done:%+v", task.GetSectorID())
	ss := task.SectorStorage
	// a fix point, link or mount to the targe file.
	if err := database.MountStorage(
		ss.StorageInfo.MountType,
		ss.StorageInfo.MountUri,
		w.sealedRepo,
		ss.StorageInfo.MountOpt,
	); err != nil {
		return errors.As(err)
	}

	if err := os.MkdirAll(filepath.Join(w.sealedRepo, "sealed"), 0755); err != nil {
		return errors.As(err)
	}
	if err := os.MkdirAll(filepath.Join(w.sealedRepo, "cache"), 0755); err != nil {
		return errors.As(err)
	}
	// "sealed" is created during previous step
	if err := w.push(ctx, "sealed", task.SectorNum); err != nil {
		return errors.As(err)
	}

	if err := w.push(ctx, "cache", task.SectorNum); err != nil {
		return errors.As(err)
	}

	if err := w.cleanCache(ctx); err != nil {
		return errors.As(err)
	}
	return nil

}

func (w *worker) pushCommit(ctx context.Context, task sectorbuilder.WorkerTask) error {
repush:
	select {
	case <-ctx.Done():
		return sectorbuilder.ErrWorkerExit.As(task)
	default:
		api, err := GetNodeApi()
		if err != nil {
			log.Warn(errors.As(err))
			goto repush
		}

		// release the worker when pushing happened
		if err := api.WorkerPushing(ctx, task.Key()); err != nil {
			log.Warn(errors.As(err))

			if errors.ErrNoData.Equal(err) {
				// drop data
				return nil
			}

			ReleaseNodeApi(false)
			goto repush
		}

		if err := w.pushCache(ctx, task); err != nil {
			log.Error(errors.As(err, task))
			time.Sleep(60e9)
			goto repush
		}
	}
	return nil
}

func (w *worker) workerDone(ctx context.Context, task sectorbuilder.WorkerTask, res sectorbuilder.SealRes) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Info("Get Node Api")
			api, err := GetNodeApi()
			if err != nil {
				log.Warn(errors.As(err))
				continue
			}
			log.Info("Do WorkerDone")
			if err := api.WorkerDone(ctx, res); err != nil {
				if errors.ErrNoData.Equal(err) {
					log.Warn("caller not found, drop this task:%+v", task)
					return
				}

				log.Warn(errors.As(err))

				ReleaseNodeApi(false)
				continue
			}

			// pass
			return

		}
	}
}

func (w *worker) processTask(ctx context.Context, task sectorbuilder.WorkerTask) sectorbuilder.SealRes {
	switch task.Type {
	case sectorbuilder.WorkerAddPiece:
	case sectorbuilder.WorkerPreCommit:
	case sectorbuilder.WorkerCommit:
	default:
		return errRes(errors.New("unknown task type").As(task.Type, w.workerCfg), task)
	}

	// if err := w.fetchSector(task.SectorID, task.Type); err != nil {
	// 	return errRes(xerrors.Errorf("fetching sector: %w", err))
	// }
	//
	// log.Infof("Data fetched, starting computation")

	res := sectorbuilder.SealRes{
		Type:      task.Type,
		TaskID:    task.Key(),
		WorkerCfg: w.workerCfg,
	}

	switch task.Type {
	case sectorbuilder.WorkerAddPiece:
		// keep cache clean, the task will lock the cache.
		if err := w.cleanCache(ctx); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		rsp, err := w.addPiece(ctx, task)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		// if err := w.push("staging", task.SectorID); err != nil {
		// 	return errRes(xerrors.Errorf("pushing unsealed data: %w", err))
		// }
		res.Piece = rsp[0]

	case sectorbuilder.WorkerPreCommit:
		// checking staging data
		stagedFile := filepath.Join(w.repo, "staging", w.sb.SectorName(task.SectorNum))
		if _, err := os.Lstat(stagedFile); err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, w.workerCfg), task)
			}

			log.Infof("not found %d local staging data, try fetch", task.SectorNum)
			// not found local staging data, try fetch
			if err := w.fetchSector(task.SectorNum, task.Type); err != nil {
				// return the err task not found and drop it.
				return errRes(sectorbuilder.ErrTaskNotFound.As(err, task), task)
			}
			// pass
		}
		rspco, err := w.sb.SealPreCommit(ctx, task.SectorNum, task.SealTicket, task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.Rspco = rspco.ToJson()
	case sectorbuilder.WorkerCommit:
		proof, err := w.sb.SealCommit(ctx, task.SectorNum, task.SealTicket, task.SealSeed, task.Pieces, task.Rspco)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		if err := w.pushCommit(ctx, task); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		res.Proof = proof
	}

	return res
}

func errRes(err error, task sectorbuilder.WorkerTask) sectorbuilder.SealRes {
	return sectorbuilder.SealRes{
		Type:   task.Type,
		TaskID: task.Key(),
		Err:    err.Error(),
		GoErr:  err,
		WorkerCfg: sectorbuilder.WorkerCfg{
			//
			ID: task.WorkerID,
		},
	}
}
