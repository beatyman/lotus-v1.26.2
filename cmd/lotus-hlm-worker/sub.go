package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	sealing "github.com/filecoin-project/lotus/extern/storage-sealing"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/gwaylib/errors"
)

type worker struct {
	minerEndpoint string
	workerRepo    string
	sealedRepo    string
	auth          http.Header

	paramsLock     sync.Mutex
	paramsVerified bool

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
			// check params
			if workerCfg.Commit2Srv || workerCfg.WdPoStSrv || workerCfg.WnPoStSrv || workerCfg.ParallelCommit2 > 0 {
				ssize, err := task.ProofType.SectorSize()
				if err != nil {
					return errors.As(err)
				}
				if err := w.CheckParams(ctx, minerEndpoint, to, ssize); err != nil {
					return errors.As(err)
				}
			}
			if task.SectorID.Miner == 0 {
				// connection is down.
				return errors.New("server shutdown").As(task)
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

	s := sealing.NewSealPiece(w.actAddr, w.workerSB)
	g := &sealing.Pledge{
		SectorID:      storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType},
		Sealing:       s,
		SectorBuilder: w.workerSB,
		ActAddr:       w.actAddr,
		Sizes:         sizes,
	}
	return g.PledgeSector(ctx)
}

func (w *worker) RemoveCache(ctx context.Context, sid string) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	log.Infof("Remove cache:%s,%s", w.workerRepo, sid)
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "sealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "cache", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "unsealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	return nil
}

func (w *worker) CleanCache(ctx context.Context) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	// not do this on miner repo
	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	sealed := filepath.Join(w.workerRepo, "sealed")
	cache := filepath.Join(w.workerRepo, "cache")
	// staged := filepath.Join(w.workerRepo, "staging")
	unsealed := filepath.Join(w.workerRepo, "unsealed")
	if err := w.cleanCache(ctx, sealed); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, cache); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, unsealed); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) cleanCache(ctx context.Context, path string) error {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		fileNames := []string{}
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		ws, err := api.WorkerWorkingById(ctx, fileNames)
		if err != nil {
			ReleaseNodeApi(false)
			return errors.As(err, fileNames)
		}
	sealedLoop:
		for _, f := range files {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue sealedLoop
				}
			}
			log.Infof("Remove %s", filepath.Join(path, f.Name()))
			if err := os.RemoveAll(filepath.Join(path, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(path, f.Name()))
			}
		}
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	return nil
}

func (w *worker) mountPush(sid, mountType, mountUri, mountDir, mountOpt string) error {
	// mount
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return errors.As(err, mountDir)
	}
	w.pushMu.Lock()
	w.sealedMounted[sid] = mountDir
	mountedData, err := json.Marshal(w.sealedMounted)
	if err != nil {
		w.pushMu.Unlock()
		return errors.As(err, w.sealedMountedFile)
	}
	if err := ioutil.WriteFile(w.sealedMountedFile, mountedData, 0666); err != nil {
		w.pushMu.Unlock()
		return errors.As(err, w.sealedMountedFile)
	}
	w.pushMu.Unlock()

	// a fix point, link or mount to the targe file.
	if err := database.Mount(
		mountType,
		mountUri,
		mountDir,
		mountOpt,
	); err != nil {
		return errors.As(err)
	}
	return nil
}

func umountAllPush(sealedMountedFile string) error {
	sealedMounted := map[string]string{}
	if mountedData, err := ioutil.ReadFile(sealedMountedFile); err == nil {
		if err := json.Unmarshal(mountedData, &sealedMounted); err != nil {
			return errors.As(err, sealedMountedFile)
		}
		for _, p := range sealedMounted {
			if _, err := database.Umount(p); err != nil {
				log.Info(err)
			} else {
				if err := os.RemoveAll(p); err != nil {
					log.Error(err)
				}
			}
		}
		return nil
	} else {
		// drop the file error
		log.Info(errors.As(err))
	}
	return nil
}

func (w *worker) umountPush(sid, mountDir string) error {
	// umount and client the tmp file
	if _, err := database.Umount(mountDir); err != nil {
		return errors.As(err)
	}
	log.Infof("Remove mount point:%s", mountDir)
	if err := os.RemoveAll(mountDir); err != nil {
		return errors.As(err)
	}

	w.pushMu.Lock()
	delete(w.sealedMounted, sid)
	mountedData, err := json.Marshal(w.sealedMounted)
	if err != nil {
		w.pushMu.Unlock()
		return errors.As(err)
	}
	if err := ioutil.WriteFile(w.sealedMountedFile, mountedData, 0666); err != nil {
		w.pushMu.Unlock()
		return errors.As(err)
	}
	w.pushMu.Unlock()
	return nil
}

var (
	pushCacheLk = sync.Mutex{}
)

func (w *worker) PushCache(ctx context.Context, task ffiwrapper.WorkerTask) error {
	sid := task.GetSectorID()
	log.Infof("PushCache:%+v", sid)
	defer log.Infof("PushCache exit:%+v", sid)

	// only can transfer one
	//pushCacheLk.Lock()
	//defer pushCacheLk.Unlock()

	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	storage, err := api.PreStorageNode(ctx, sid, w.workerCfg.IP)
	if err != nil {
		return errors.As(err)
	}
	mountUri := storage.MountTransfUri
	if strings.Index(mountUri, w.workerCfg.IP) > -1 {
		log.Infof("found local storage, chagne %s to mount local", mountUri)
		// fix to 127.0.0.1 if it has the same ip.
		mountUri = strings.Replace(mountUri, w.workerCfg.IP, "127.0.0.1", -1)
	}
	mountDir := filepath.Join(w.sealedRepo, sid)
	if err := w.mountPush(
		sid,
		storage.MountType,
		mountUri,
		mountDir,
		storage.MountOpt,
	); err != nil {
		return errors.As(err)
	}
	sealedPath := filepath.Join(mountDir, "sealed")
	if err := os.MkdirAll(sealedPath, 0755); err != nil {
		return errors.As(err)
	}
	// "sealed" is created during previous step
	if err := w.pushRemote(ctx, "sealed", sid, filepath.Join(sealedPath, sid)); err != nil {
		return errors.As(err)
	}
	cachePath := filepath.Join(mountDir, "cache", sid)
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return errors.As(err)
	}
	if err := w.pushRemote(ctx, "cache", sid, cachePath); err != nil {
		return errors.As(err)
	}
	if err := w.umountPush(sid, mountDir); err != nil {
		return errors.As(err)
	}
	if err := api.CommitStorageNode(ctx, sid); err != nil {
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
		if err := api.WorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "pushing commit", database.SECTOR_STATE_PUSH); err != nil {
			log.Warn(errors.As(err))

			if errors.ErrNoData.Equal(err) {
				// drop data
				return nil
			}

			ReleaseNodeApi(false)
			goto repush
		}

		if err := w.PushCache(ctx, task); err != nil {
			log.Error(errors.As(err, task))
			time.Sleep(60e9)
			goto repush
		}
		if err := w.RemoveCache(ctx, task.GetSectorID()); err != nil {
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
	if w.workerCfg.CacheMode == 0 &&
		task.Type > ffiwrapper.WorkerAddPiece && task.Type < ffiwrapper.WorkerCommit2 &&
		task.WorkerID != w.workerCfg.ID {
		// lock bandwidth
		if err := api.WorkerAddConn(ctx, task.WorkerID, 1); err != nil {
			ReleaseNodeApi(false)
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	retryFetch:
		// fetch data
		fromMiner := false
		uri := task.SectorStorage.WorkerInfo.SvcUri
		if len(uri) == 0 {
			uri = w.minerEndpoint
			fromMiner = true
		}
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
		// keep unseal data from miner
		if !fromMiner {
			// release the storage cache
			log.Infof("fetch %s done, try delete remote files.", task.Key())
			if err := w.deleteRemoteCache(
				"http://"+uri,
				task.SectorStorage.SectorInfo.ID,
				"all",
			); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	}
	// lock the task to this worker
	if err := api.WorkerLock(ctx, w.workerCfg.ID, task.Key(), "task in", int(task.Type)); err != nil {
		ReleaseNodeApi(false)
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	unlockWorker := false
	switch task.Type {
	case ffiwrapper.WorkerAddPiece:
		rsp, err := w.addPiece(ctx, task)
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
		rspco, err := w.workerSB.SealPreCommit1(ctx, storage.SectorRef{
			ID:        task.SectorID,
			ProofType: task.ProofType,
		}, task.SealTicket, pieceInfo)

		//rspco, err := ExecPrecommit1(ctx, w.workerRepo, task)
		res.PreCommit1Out = rspco
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}

		// checking is the next step interrupted
		unlockWorker = (w.workerCfg.ParallelPrecommit2 == 0)
	case ffiwrapper.WorkerPreCommit2:
		out, err := w.workerSB.SealPreCommit2(ctx, storage.SectorRef{
			ID:        task.SectorID,
			ProofType: task.ProofType,
		}, task.PreCommit1Out)
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
		out, err := w.workerSB.SealCommit1(ctx, storage.SectorRef{
			ID:        task.SectorID,
			ProofType: task.ProofType,
		}, task.SealTicket, task.SealSeed, pieceInfo, *cids)
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
			res.Commit2Out, err = w.workerSB.SealCommit2(ctx, storage.SectorRef{
				ID:        task.SectorID,
				ProofType: task.ProofType,
			}, task.Commit1Out)
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	// SPEC: cancel deal with worker finalize, because it will post failed when commit2 is online and finalize is interrupt.
	// SPEC: maybe it should failed on commit2 but can not failed on transfering the finalize data on windowpost.
	// TODO: when testing stable finalize retrying and reopen it.
	case ffiwrapper.WorkerFinalize:
		sealedFile := w.workerSB.SectorPath("sealed", task.GetSectorID())
		_, err := os.Stat(string(sealedFile))
		if err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, sealedFile), &res)
			}
		} else {
			if err := w.workerSB.FinalizeSector(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, nil); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			if err := w.pushCommit(ctx, task); err != nil {
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

func errRes(err error, res *ffiwrapper.SealRes) ffiwrapper.SealRes {
	if err != nil {
		res.Err = err.Error()
		res.GoErr = err
	}
	return *res
}
