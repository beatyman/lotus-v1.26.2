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

	actAddr address.Address

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
	fileServer string,
	repo, sealedRepo string,
	cacheSectors uint,
	noAddPiece, noPrecommit1, noPrecommit2, noCommit1, noCommit2, noWPoSt, noPoSt bool,
) error {
	workerId := GetWorkerID(repo)
	netIp := os.Getenv("NETIP")

	workerCfg := ffiwrapper.WorkerCfg{
		ID:           workerId,
		IP:           netIp,
		SvcUri:       fileServer,
		MaxCacheNum:  cacheSectors,
		NoAddPiece:   noAddPiece,
		NoPrecommit1: noPrecommit1,
		NoPrecommit2: noPrecommit2,
		NoCommit1:    noCommit1,
		NoCommit2:    noCommit2,
		NoWPoSt:      noWPoSt,
		NoPoSt:       noPoSt,
	}

	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	w := &worker{
		minerEndpoint: endpoint,
		auth:          auth,
		repo:          repo,
		sealedRepo:    sealedRepo,

		sb: sb,

		actAddr:   act,
		sealedSB:  sealedSB,
		workerCfg: workerCfg,
		workOn:    map[string]ffiwrapper.WorkerTask{},
	}

	tasks, err := api.WorkerQueue(ctx, workerCfg)
	if err != nil {
		return err
	}
	log.Infof("Worker(%s) started", workerId)

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
				w.workerDone(ctx, task, res)

				log.Infof("Task %s done, err: %+v", task.Key(), res.GoErr)
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

	s := sealing.NewSealPiece(w.actAddr, w.sb)
	g := &sealing.Pledge{
		SectorID:      task.SectorID,
		Sealing:       s,
		SectorBuilder: w.sb,
		ActAddr:       w.actAddr,
		Sizes:         sizes,
	}
	return g.PledgeSector(ctx)
}

func (w *worker) removeCache(ctx context.Context, sid string) error {
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

	if err := os.RemoveAll(filepath.Join(w.repo, "sealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.repo, "cache", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.repo, "unsealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	return nil
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
			log.Infof("clean sealed:%s", filepath.Join(sealed, f.Name()))
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
			log.Infof("clean cache:%s", filepath.Join(cache, f.Name()))
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
			log.Infof("clean unsealed:%s", filepath.Join(unsealed, f.Name()))
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
		// TODO: check cache is support two task
		api, err := GetNodeApi()
		if err != nil {
			log.Warn(errors.As(err))
			goto repush
		}
		// release the worker when pushing happened
		if err := api.WorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "pushing commit"); err != nil {
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
		if err := w.removeCache(ctx, task.GetSectorID()); err != nil {
			log.Warn(errors.As(err))
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
	res := ffiwrapper.SealRes{
		Type:      task.Type,
		TaskID:    task.Key(),
		WorkerCfg: w.workerCfg,
	}

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
	api, err := GetNodeApi()
	if err != nil {
		ReleaseNodeApi(false)
		return errRes(errors.As(err, w.workerCfg), task)
	}
	// keep cache clean, the task will lock the cache.
	if err := w.cleanCache(ctx); err != nil {
		return errRes(errors.As(err, w.workerCfg), task)
	}
	// checking is the cache in a different storage server, do fetch when it is.
	if task.Type < ffiwrapper.WorkerCommit2 && len(task.WorkerID) > 0 && task.WorkerID != w.workerCfg.ID {
		// lock bandwidth
		if err := api.WorkerAddConn(ctx, task.WorkerID, 1); err != nil {
			ReleaseNodeApi(false)
			return errRes(errors.As(err, w.workerCfg), task)
		}
	retryFetch:
		// fetch data
		uri := task.SectorStorage.WorkerInfo.SvcUri
		if len(uri) == 0 {
			uri = w.minerEndpoint
		}
		if err := w.fetch(
			uri,
			task.SectorStorage.SectorInfo.ID,
			task.Type,
		); err != nil {
			log.Warnf("fileserver error, retry 10s later:%+s", err.Error())
			time.Sleep(10e9)
			goto retryFetch
		}
		// release bandwidth
		if err := api.WorkerAddConn(ctx, task.WorkerID, -1); err != nil {
			ReleaseNodeApi(false)
			return errRes(errors.As(err, w.workerCfg), task)
		}
		// release the storage cache
		log.Infof("fetch %s done, try delete remote files.", task.Key())
		if err := w.deleteCache(
			uri,
			task.SectorStorage.SectorInfo.ID,
		); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
	}
	// lock the task to this worker
	if err := api.WorkerLock(ctx, w.workerCfg.ID, task.Key(), "task in", int(task.Type)); err != nil {
		ReleaseNodeApi(false)
		return errRes(errors.As(err, w.workerCfg), task)
	}
	unlockWorker := false
	switch task.Type {
	case ffiwrapper.WorkerAddPiece:
		rsp, err := w.addPiece(ctx, task)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.Pieces = rsp

		// checking is the next step interrupted
		unlockWorker = w.workerCfg.NoPrecommit1

	case ffiwrapper.WorkerPreCommit1:
		pieceInfo, err := ffiwrapper.DecodePieceInfo(task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		rspco, err := w.sb.SealPreCommit1(ctx, task.SectorID, task.SealTicket, pieceInfo)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.PreCommit1Out = rspco

		// checking is the next step interrupted
		unlockWorker = w.workerCfg.NoPrecommit2
	case ffiwrapper.WorkerPreCommit2:
		out, err := w.sb.SealPreCommit2(ctx, task.SectorID, task.PreCommit1Out)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.PreCommit2Out = ffiwrapper.SectorCids{
			Unsealed: out.Unsealed.String(),
			Sealed:   out.Sealed.String(),
		}

		// checking is the next step interrupted
		unlockWorker = w.workerCfg.NoCommit1
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
		// checking is the next step interrupted
		unlockWorker = w.workerCfg.NoCommit2
	case ffiwrapper.WorkerCommit2:
		// clean unsealed
		if err := os.RemoveAll(filepath.Join(w.repo, "unsealed", task.GetSectorID())); err != nil {
			log.Error(errors.As(err, task.GetSectorID()))
		}

		out, err := w.sb.SealCommit2(ctx, task.SectorID, task.Commit1Out)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
		res.Commit2Out = out
		// SPEC: cancel deal with worker finalize, because it will post failed when commit2 is online and finalize is interrupt.
		// SPEC: maybe it should failed on commit2 but can not failed on transfering the finalize data on windowpost.
		// TODO: when testing stable finalize retrying and reopen it.
		// case ffiwrapper.WorkerFinalize:
		if err := w.sb.FinalizeSector(ctx, task.SectorID, nil); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}

		if err := w.pushCommit(ctx, task); err != nil {
			return errRes(errors.As(err, w.workerCfg), task)
		}
	}

	// release the worker when stage is interrupted
	if unlockWorker {
		log.Info("Release Worker by:", task)
		if err := api.WorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "transfer to another worker"); err != nil {
			log.Warn(errors.As(err))
			ReleaseNodeApi(false)
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
