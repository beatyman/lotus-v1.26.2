package main

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/gwaylib/errors"
)

type worker struct {
	minerEndpoint string
	workerRepo    string
	sealedRepo    string
	auth          http.Header

	paramsLock   sync.Mutex
	needCheckSum bool

	ssize   abi.SectorSize
	actAddr address.Address

	workerSB  *ffiwrapper.Sealer
	rpcServer *rpcServer
	workerCfg ffiwrapper.WorkerCfg

	workMu sync.Mutex
	workOn map[string]ffiwrapper.WorkerTask // task key

	pushMu            sync.Mutex
	sealedMounted     map[string]string
	sealedMountedFile string
}

func acceptJobs(ctx context.Context,
	workerSB, sealedSB *ffiwrapper.Sealer,
	rpcServer *rpcServer,
	act, workerAddr address.Address,
	minerEndpoint string, auth http.Header,
	workerRepo, sealedRepo, mountedFile string,
	workerCfg ffiwrapper.WorkerCfg,
) error {
	w := &worker{
		minerEndpoint: minerEndpoint,
		workerRepo:    workerRepo,
		sealedRepo:    sealedRepo,
		auth:          auth,

		actAddr:   act,
		workerSB:  workerSB,
		rpcServer: rpcServer,
		workerCfg: workerCfg,

		workOn: map[string]ffiwrapper.WorkerTask{},

		sealedMounted:     map[string]string{},
		sealedMountedFile: mountedFile,
	}

checkingApi:
	api, err := GetNodeApi()
	if err != nil {
		log.Warn(errors.As(err))
		time.Sleep(3e9)
		goto checkingApi
	}

	to := "/var/tmp/filecoin-proof-parameters"
	envParam := os.Getenv("FIL_PROOFS_PARAMETER_CACHE")
	if len(envParam) > 0 {
		to = envParam
	}
	if workerCfg.Commit2Srv || workerCfg.WdPoStSrv || workerCfg.WnPoStSrv || workerCfg.ParallelCommit2 > 0 {
		// get ssize from miner
		ssize, err := nodeApi.ActorSectorSize(ctx, act)
		if err != nil {
			return err
		}
		if err := w.CheckParams(ctx, minerEndpoint, to, ssize); err != nil {
			return err
		}
	}

	tasks, err := api.WorkerQueue(ctx, workerCfg)
	if err != nil {
		return errors.As(err)
	}
	log.Infof("Worker(%s) started, Miner:%s, Srv:%s", workerCfg.ID, minerEndpoint, workerCfg.IP)

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
			// TODO: check params for task proof type.
			//if workerCfg.Commit2Srv || workerCfg.WdPoStSrv || workerCfg.WnPoStSrv || workerCfg.ParallelCommit2 > 0 {
			//	ssize, err := task.ProofType.SectorSize()
			//	if err != nil {
			//		return errors.As(err)
			//	}
			//	if err := w.CheckParams(ctx, minerEndpoint, to, ssize); err != nil {
			//		return errors.As(err)
			//	}
			//}
			if task.SectorID.Miner == 0 {
				// connection is down.
				return errors.New("server shutdown").As(task)
			}

			log.Infof("New task: %s, sector %s, action: %d", task.Key(), task.SectorName(), task.Type)
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

func errRes(err error, res *ffiwrapper.SealRes) ffiwrapper.SealRes {
	if err != nil {
		res.Err = err.Error()
		res.GoErr = err
	}
	return *res
}

func (w *worker) processTask(ctx context.Context, task ffiwrapper.WorkerTask) ffiwrapper.SealRes {
	res := ffiwrapper.SealRes{
		Type:      task.Type,
		TaskID:    task.Key(),
		WorkerCfg: w.workerCfg,
	}

	switch task.Type {
	case ffiwrapper.WorkerPledge:
	case ffiwrapper.WorkerPreCommit1:
	case ffiwrapper.WorkerPreCommit2:
	case ffiwrapper.WorkerCommit1:
	case ffiwrapper.WorkerCommit2:
	case ffiwrapper.WorkerFinalize:
	case ffiwrapper.WorkerWindowPoSt:
		proofs, err := w.rpcServer.GenerateWindowPoSt(ctx,
			task.SectorID.Miner,
			task.SectorInfo,
			task.Randomness,
		)
		res.WindowPoStProofOut = proofs.Proofs
		res.WindowPoStIgnSectors = proofs.Ignore
		return errRes(err, &res)
	case ffiwrapper.WorkerWinningPoSt:
		proofs, err := w.rpcServer.GenerateWinningPoSt(ctx,
			task.SectorID.Miner,
			task.SectorInfo,
			task.Randomness,
		)
		res.WinningPoStProofOut = proofs
		return errRes(err, &res)
	default:
		return errRes(errors.New("unknown task type").As(task.Type, w.workerCfg), &res)
	}
	api, err := GetNodeApi()
	if err != nil {
		ReleaseNodeApi(false)
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	// clean cache before working.
	if err := w.CleanCache(ctx); err != nil {
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	// checking is the cache in a different storage server, do fetch when it is.
	if w.workerCfg.CacheMode == 0 {
		// fetch the precommit cache data
		if task.Type > ffiwrapper.WorkerPledge && task.Type < ffiwrapper.WorkerCommit2 && task.WorkerID != w.workerCfg.ID {
			// lock bandwidth
			if err := api.WorkerAddConn(ctx, task.WorkerID, 1); err != nil {
				ReleaseNodeApi(false)
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		retryFetch:
			// fetch data
			uri := task.SectorStorage.WorkerInfo.SvcUri
			if err := w.fetchRemote(
				"http://"+uri,
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
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			// release the storage cache
			log.Infof("fetch %s done, try delete remote files.", task.Key())
			if err := w.deleteRemoteCache(
				"http://"+uri,
				task.SectorStorage.SectorInfo.ID,
				"all",
			); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		} else if task.Type == ffiwrapper.WorkerPledge && len(task.ExtSizes) > 0 {
			// get the market unsealed data, and copy to local
			if err := w.fetchPledge(ctx, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			// fetch done
		}
	}
	// lock the task to this worker
	if err := api.WorkerLock(ctx, w.workerCfg.ID, task.Key(), "task in", int(task.Type)); err != nil {
		ReleaseNodeApi(false)
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	unlockWorker := false

	sector := storage.SectorRef{
		ID:        task.SectorID,
		ProofType: task.ProofType,
		SectorFile: storage.SectorFile{
			SectorId:    storage.SectorName(task.SectorID),
			StorageRepo: w.workerSB.RepoPath(),
		},
	}
	switch task.Type {
	case ffiwrapper.WorkerPledge:
		rsp, err := w.workerSB.PledgeSector(ctx,
			sector,
			task.ExistingPieceSizes,
			task.ExtSizes...,
		)

		res.Pieces = rsp
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}

		// checking is the next step interrupted
		unlockWorker = (w.workerCfg.ParallelPrecommit1 == 0)

	case ffiwrapper.WorkerPreCommit1:
		pieceInfo, err := ffiwrapper.DecodePieceInfo(task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		rspco, err := w.workerSB.SealPreCommit1(ctx, sector, task.SealTicket, pieceInfo)

		// rspco, err := ffiwrapper.ExecPrecommit1(ctx, w.workerRepo, task)
		res.PreCommit1Out = rspco
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}

		// checking is the next step interrupted
		unlockWorker = (w.workerCfg.ParallelPrecommit2 == 0)
	case ffiwrapper.WorkerPreCommit2:
		out, err := w.workerSB.SealPreCommit2(ctx, sector, task.PreCommit1Out)
		//out, err := ffiwrapper.ExecPrecommit2(ctx, w.workerRepo, task)
		res.PreCommit2Out = ffiwrapper.SectorCids{
			Unsealed: out.Unsealed.String(),
			Sealed:   out.Sealed.String(),
		}
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	case ffiwrapper.WorkerCommit1:
		pieceInfo, err := ffiwrapper.DecodePieceInfo(task.Pieces)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		cids, err := task.Cids.Decode()
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		out, err := w.workerSB.SealCommit1(ctx, sector, task.SealTicket, task.SealSeed, pieceInfo, *cids)
		res.Commit1Out = out
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	case ffiwrapper.WorkerCommit2:
		var err error
		// if local no gpu service, using remote if the remtoes have.
		// TODO: Optimized waiting algorithm
		if w.workerCfg.ParallelCommit2 == 0 && !w.workerCfg.Commit2Srv {
			for {
				res.Commit2Out, err = CallCommit2Service(ctx, task)
				if err != nil {
					log.Warn(errors.As(err))
					time.Sleep(10e9)
					continue
				}
				break
			}
		}
		// call gpu service failed, using local instead.
		if len(res.Commit2Out) == 0 {
			res.Commit2Out, err = w.workerSB.SealCommit2(ctx, sector, task.Commit1Out)
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	// SPEC: cancel deal with worker finalize, because it will post failed when commit2 is online and finalize is interrupt.
	// SPEC: maybe it should failed on commit2 but can not failed on transfering the finalize data on windowpost.
	// TODO: when testing stable finalize retrying and reopen it.
	case ffiwrapper.WorkerFinalize:
		sealedFile := w.workerSB.SectorPath("sealed", task.SectorName())
		_, err := os.Stat(string(sealedFile))
		if err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, sealedFile), &res)
			}
		} else {
			if err := w.workerSB.FinalizeSector(ctx, sector, nil); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			if err := w.pushCache(ctx, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	}

	// release the worker when stage is interrupted
	if unlockWorker {
		log.Info("Release Worker by:", task)
		if err := api.WorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "transfer to another worker", database.SECTOR_STATE_MOVE); err != nil {
			log.Warn(errors.As(err))
			ReleaseNodeApi(false)
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	}
	return res
}
