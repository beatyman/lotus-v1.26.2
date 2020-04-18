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
	"github.com/filecoin-project/sector-storage/database"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/abi"
	sealing "github.com/filecoin-project/storage-fsm"

	"github.com/gwaylib/errors"
)

type worker struct {
	minerEndpoint string
	repo          string
	sealedRepo    string
	auth          http.Header

	actAddr    address.Address
	workerAddr address.Address

	sb        *ffiwrapper.Sealer
	sealedSB  *ffiwrapper.Sealer
	workerCfg ffiwrapper.WorkerCfg

	workMu sync.Mutex
	workOn map[string]ffiwrapper.WorkerTask // task key
}

func acceptJobs(ctx context.Context,
	sb, sealedSB *ffiwrapper.Sealer,
	act, workerAddr address.Address,
	endpoint string, auth http.Header,
	repo, sealedRepo string,
	noAddPiece, noSeal, noVerify bool,
) error {
	workerId := GetWorkerID(repo)
	netIp := os.Getenv("NETIP")

	workerCfg := ffiwrapper.WorkerCfg{
		ID:         workerId,
		IP:         netIp,
		NoAddPiece: noAddPiece,
		NoSeal:     noSeal,
		NoVerify:   noVerify,
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
		workOn:     map[string]ffiwrapper.WorkerTask{},
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
			if task.SectorID.Miner == 0 {
				// connection is down.
				ReleaseNodeApi(false)
				return errors.New("Error miner id").As(task)
			}

			log.Infof("New task: %s, sector %s, action: %d", task.Key(), task.GetSectorID(), task.Type)
			go func(task ffiwrapper.WorkerTask) {
				taskKey := task.Key()
				w.workMu.Lock()
				if _, ok := w.workOn[taskKey]; ok {
					w.workMu.Unlock()
					// when the miner restart, it should send the same task,
					// and this worker is already working on, so drop this job.
					log.Infof("task(%s) is in working", taskKey)
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

func (w *worker) addPiece(ctx context.Context, task ffiwrapper.WorkerTask) ([]abi.PieceInfo, error) {
	sizes := task.PieceSizes

	s := sealing.NewSealPiece(w.actAddr, w.workerAddr, w.sb)
	g := &sealing.Pledge{
		SectorID:      task.SectorID,
		Sealing:       s,
		SectorBuilder: w.sb,
		ActAddr:       w.actAddr,
		WorkerAddr:    w.workerAddr,
		Sizes:         sizes,
	}
	return g.PledgeSector(ctx)
}

func (w *worker) cleanCache(ctx context.Context) error {
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

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
	// staged := filepath.Join(w.repo, "staging")
	unsealed := filepath.Join(w.repo, "unsealed")

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
	unsealedFiles, err := ioutil.ReadDir(unsealed)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
	stagedLoop:
		for _, f := range unsealedFiles {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue stagedLoop
				}
			}
			if err := os.RemoveAll(filepath.Join(unsealed, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(unsealed, f.Name()))
			}
		}
	}

	if err := os.MkdirAll(sealed, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	if err := os.MkdirAll(cache, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	if err := os.MkdirAll(unsealed, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	return nil
}

func (w *worker) pushCache(ctx context.Context, task ffiwrapper.WorkerTask) error {
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
	// "sealed" is created during previous step
	if err := w.push(ctx, "sealed", ffiwrapper.SectorName(task.SectorID)); err != nil {
		return errors.As(err)
	}
	if err := os.MkdirAll(filepath.Join(w.sealedRepo, "cache"), 0755); err != nil {
		return errors.As(err)
	}
	if err := w.push(ctx, "cache", ffiwrapper.SectorName(task.SectorID)); err != nil {
		return errors.As(err)
	}
	// if err := os.MkdirAll(filepath.Join(w.sealedRepo, "unsealed"), 0755); err != nil {
	// 	return errors.As(err)
	// }
	// if err := w.push(ctx, "unsealed", ffiwrapper.SectorName(task.SectorID)); err != nil {
	// 	return errors.As(err)
	// }
	if err := database.Umount(w.sealedRepo); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) pushCommit(ctx context.Context, task ffiwrapper.WorkerTask) error {
repush:
	select {
	case <-ctx.Done():
		return ffiwrapper.ErrWorkerExit.As(task)
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
		if err := w.cleanCache(ctx); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

func (w *worker) workerDone(ctx context.Context, task ffiwrapper.WorkerTask, res ffiwrapper.SealRes) {
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

func (w *worker) processTask(ctx context.Context, task ffiwrapper.WorkerTask) ffiwrapper.SealRes {
	switch task.Type {
	case ffiwrapper.WorkerAddPiece:
	case ffiwrapper.WorkerPreCommit1:
	case ffiwrapper.WorkerPreCommit2:
	case ffiwrapper.WorkerCommit1:
	case ffiwrapper.WorkerCommit2:
	case ffiwrapper.WorkerFinalize:
	default:
		return errRes(errors.New("unknown task type").As(task.Type, w.workerCfg), task)
	}

	res := ffiwrapper.SealRes{
		Type:      task.Type,
		TaskID:    task.Key(),
		WorkerCfg: w.workerCfg,
	}

	switch task.Type {
	case ffiwrapper.WorkerAddPiece:
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
		res.Pieces = rsp

	case ffiwrapper.WorkerPreCommit1:
		// checking staging data
		unsealedFile := filepath.Join(w.repo, "unsealed", w.sb.SectorName(task.SectorID))
		if _, err := os.Lstat(unsealedFile); err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, w.workerCfg), task)
			}

			log.Infof("not found %d local staging data, try fetch", task.SectorID)
			// not found local staging data, try fetch
			if err := w.fetchSector(task.SectorID, task.Type); err != nil {
				// return the err task not found and drop it.
				return errRes(ffiwrapper.ErrTaskNotFound.As(err, task), task)
			}
			// pass
		}
		pieceInfo, err := ffiwrapper.DecodePieceInfo(task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		rspco, err := w.sb.SealPreCommit1(ctx, task.SectorID, task.SealTicket, pieceInfo)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.PreCommit1Out = rspco
	case ffiwrapper.WorkerPreCommit2:
		out, err := w.sb.SealPreCommit2(ctx, task.SectorID, task.PreCommit1Out)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.PreCommit2Out = ffiwrapper.SectorCids{
			Unsealed: out.Unsealed.String(),
			Sealed:   out.Sealed.String(),
		}
	case ffiwrapper.WorkerCommit1:
		pieceInfo, err := ffiwrapper.DecodePieceInfo(task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		cids, err := task.Cids.Decode()
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		out, err := w.sb.SealCommit1(ctx, task.SectorID, task.SealTicket, task.SealSeed, pieceInfo, *cids)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.Commit1Out = out
	case ffiwrapper.WorkerCommit2:
		out, err := w.sb.SealCommit2(ctx, task.SectorID, task.Commit1Out)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.Commit2Out = out
		// case ffiwrapper.WorkerFinalize:
		if err := w.sb.FinalizeSector(ctx, task.SectorID); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		if err := w.pushCommit(ctx, task); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
	}

	return res
}

func errRes(err error, task ffiwrapper.WorkerTask) ffiwrapper.SealRes {
	return ffiwrapper.SealRes{
		Type:   task.Type,
		TaskID: task.Key(),
		Err:    err.Error(),
		GoErr:  err,
		WorkerCfg: ffiwrapper.WorkerCfg{
			//
			ID: task.WorkerID,
		},
	}
}
