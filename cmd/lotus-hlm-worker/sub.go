package main

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/lotus/buried/utils"
	"github.com/google/uuid"
	"github.com/gwaylib/hardware/bindgpu"
	"go.opencensus.io/trace"
	"go.opencensus.io/trace/propagation"
	"huangdong2012/filecoin-monitor/trace/spans"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
)

type worker struct {
	minerEndpoint string
	sealedRepo    string
	auth          http.Header

	paramsLock   sync.Mutex
	needCheckSum bool

	ssize   abi.SectorSize
	actAddr address.Address

	diskPool  DiskPool
	rpcServer *rpcServer
	workerCfg ffiwrapper.WorkerCfg

	workMu  sync.RWMutex
	workOn  map[string]ffiwrapper.WorkerTask // task key
	sealers map[string]*ffiwrapper.Sealer

	pushMu           sync.Mutex
	sealedMounted    map[string]string
	sealedMountedCfg string
}

func acceptJobs(ctx context.Context,
	rpcServer *rpcServer,
	act, workerAddr address.Address,
	minerEndpoint string, auth http.Header,
	workerRepo, sealedRepo, mountedCfg string,
	workerCfg ffiwrapper.WorkerCfg,
) error {
	api, err := GetNodeApi()
	if err != nil {
		log.Warn(errors.As(err))
	}

	// get ssize from miner
	ssize, err := api.RetryActorSectorSize(ctx, act)
	if err != nil {
		return err
	}

	diskPool := NewDiskPool(ssize, workerCfg, workerRepo)
	mapstr, err := diskPool.ShowExt()
	if err == nil {
		log.Infof("new diskPool instance, worker ssd -> sector map tupple is:\r\n %+v", mapstr)
	}
	w := &worker{
		minerEndpoint: minerEndpoint,
		sealedRepo:    sealedRepo,
		auth:          auth,

		ssize:     ssize,
		actAddr:   act,
		diskPool:  diskPool,
		rpcServer: rpcServer,
		workerCfg: workerCfg,

		workOn:  map[string]ffiwrapper.WorkerTask{},
		sealers: map[string]*ffiwrapper.Sealer{},

		sealedMounted:    map[string]string{},
		sealedMountedCfg: mountedCfg,
	}

	to := "/var/tmp/filecoin-proof-parameters"
	envParam := os.Getenv("FIL_PROOFS_PARAMETER_CACHE")
	if len(envParam) > 0 {
		to = envParam
	}

	// check gpu
	if workerCfg.ParallelPrecommit2 > 0 || workerCfg.ParallelCommit > 0 || workerCfg.Commit2Srv || workerCfg.WdPoStSrv {
		bindgpu.AssertGPU(ctx)
	}

	// check params
	if workerCfg.Commit2Srv || workerCfg.WdPoStSrv || workerCfg.WnPoStSrv || workerCfg.ParallelCommit > 0 {
		if err := w.CheckParams(ctx, minerEndpoint, to, ssize); err != nil {
			return err
		}
	}

	if err = w.initDisk(ctx); err != nil {
		return err
	}

	workerCfg.Cycle = uuid.New().String() //唯一标识一次启动
	for i := 0; true; i++ {
		if i > 0 {
			<-time.After(time.Second * 10)
		}

		log.Infof("Worker(%s) starting(%v), Miner:%s, Srv:%s", workerCfg.ID, i, minerEndpoint, workerCfg.IP)
		workerCfg.Retry = i
		workerCfg.Busy = w.busyTasks()
		workerCfg.C2Sids = rpcServer.getC2sids()
		tasks, err := api.WorkerQueue(ctx, workerCfg)
		if err != nil {
			log.Infof("Worker(%s) start(%v) error(%v), Miner:%s, Srv:%s", workerCfg.ID, i, err, minerEndpoint, workerCfg.IP)
			continue
		}

		log.Infof("Worker(%s) started(%v), Miner:%s, Srv:%s", workerCfg.ID, i, minerEndpoint, workerCfg.IP)
		if err = w.processJobs(ctx, tasks); err != nil {
			log.Errorf("processJobs error(%v): %v", i, err.Error())
			continue
		} else {
			break
		}
	}

	return nil
}

func (w *worker) busyTasks() []string {
	w.workMu.RLock()
	defer w.workMu.RUnlock()

	out := make([]string, 0, 0)
	for _, task := range w.workOn {
		out = append(out, task.SectorName())
	}
	return out
}

func (w *worker) initDisk(ctx context.Context) error {
	api, _ := GetNodeApi()
	diskSectors, err := scanDisk()
	if err != nil {
		return errors.As(err)
	}

	for _, sectors := range diskSectors {
		for sid, _ := range sectors {
			info, err := api.RetryHlmSectorGetState(ctx, sid)
			if err != nil {
				log.Error("NewAllocate ", err)
				continue
			}
			//已经做完C2
			if info.State >= ffiwrapper.WorkerCommitDone {
				localSectors.WriteMap(sid, info.State)
			} else if info.State <= ffiwrapper.WorkerPreCommit2Done {
				//还做完P2 算500G
				localSectors.WriteMap(sid, info.State)
			} else {
				localSectors.WriteMap(sid, info.State)
				//还在做C1,或者C2,如果本地map没有标记C1,没做完算500G,已经做完算50G
			}
		}
	}
	log.Infof("scanDisk : %v", diskSectors)
	return nil
}

func (w *worker) processJobs(ctx context.Context, tasks <-chan ffiwrapper.WorkerTask) error {
loop:
	for {
		select {
		case task, ok := <-tasks:
			if !ok {
				return fmt.Errorf("tasks chan closed")
			}
			if task.SectorID.Miner == 0 {
				return errors.New("task invalid").As(task)
			}

			log.Infof("New task: %s, sector %s, action: %d, busy: %v", task.Key(), task.SectorName(), task.Type, w.busyTasks())
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

	return nil
}

func (w *worker) workerDone(ctx context.Context, task ffiwrapper.WorkerTask, res ffiwrapper.SealRes) {
	api, err := GetNodeApi()
	if err != nil {
		log.Warn(errors.As(err))
	}
	if err := api.RetryWorkerDone(ctx, res); err != nil {
		if errors.ErrNoData.Equal(err) {
			err = fmt.Errorf("caller not found, drop this task")
		}
		log.Errorf("Worker done error: worker(%v)  sector(%v)  error(%v)", task.WorkerID, task.SectorID, err)
	} else {
		log.Infof("Worker done success: worker(%v)  sector(%v)", task.WorkerID, task.SectorID)
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
	_, span := spans.NewTaskSpan(propagation.Extract(task.TraceContext))
	span.SetMinerID(w.actAddr.String())
	span.SetType(fmt.Sprintf("%v", int(task.Type)))
	span.SetWorkIP(w.workerCfg.IP)
	span.SetWorkNo(w.workerCfg.ID)
	span.SetMaxTaskCount(int64(w.workerCfg.MaxTaskNum))
	span.SetWindowPostEnable(w.workerCfg.WdPoStSrv)
	span.SetWinningPostEnable(w.workerCfg.WnPoStSrv)
	span.AddAttributes(trace.BoolAttribute("snap", task.Snap))
	span.Starting("task start...")

	res := ffiwrapper.SealRes{
		Type:      task.Type,
		TaskID:    task.Key(),
		WorkerCfg: w.workerCfg,
	}
	defer span.Finish(res.GoErr)

	switch task.Type {
	case ffiwrapper.WorkerPledge:
	case ffiwrapper.WorkerPreCommit1:
	case ffiwrapper.WorkerPreCommit2:
	case ffiwrapper.WorkerCommit:
	case ffiwrapper.WorkerFinalize:
	case ffiwrapper.WorkerUnseal:
	case ffiwrapper.WorkerReplicaUpdate:
	case ffiwrapper.WorkerProveReplicaUpdate1:
	case ffiwrapper.WorkerProveReplicaUpdate2:
	case ffiwrapper.WorkerGenerateSectorKeyFromData:
	case ffiwrapper.WorkerFinalizeReplicaUpdate:
	case ffiwrapper.WorkerWindowPoSt:
		proofs, err := w.rpcServer.GenerateWindowPoSt(ctx,
			task.SectorID.Miner,
			task.PostProofType,
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
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	// clean cache before working.
	if err := w.GcRepoSectors(ctx); err != nil {
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	cleanTimes := 0
	// allocate worker sealer
reAllocate:
	w.workMu.Lock()
	dpState, err := w.diskPool.NewAllocate(task.SectorName())
	if err != nil {
		w.workMu.Unlock()
		//log.Warn(errors.As(err))
		time.Sleep(60e9)
		cleanTimes++
		if cleanTimes%10 == 0 {
			if err := w.GcRepoSectors(ctx); err != nil {
				log.Warn(errors.As(err, w.workerCfg), &res)
			}
		}
		goto reAllocate
	}
	sealer, ok := w.sealers[dpState.MountPoint]
	if !ok {
		sealer, err = ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: dpState.MountPoint,
		})
		if err != nil {
			w.workMu.Unlock()
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	}
	w.sealers[dpState.MountPoint] = sealer
	w.workMu.Unlock()

	mapstr, err := w.diskPool.ShowExt()
	if err == nil {
		log.Infof("accept one new task, really time worker ssd -> sector map tupple is:\r\n %+v", mapstr)
	}

	if w.workerCfg.CacheMode == 0 {
		// fetch the unseal sector
		switch task.Type {
		// fetch done
		case ffiwrapper.WorkerUnseal:
			// get the unsealed and sealed data to do unseal operate.
			if err := w.fetchUnseal(ctx, sealer, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			if err := w.fetchSealed(ctx, sealer, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			// fetch done
		case ffiwrapper.WorkerReplicaUpdate:
			if err := w.fetchSealed(ctx, sealer, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			// fetch done
		default:
			/*
				if task.Type == ffiwrapper.WorkerPledge || task.Type == ffiwrapper.WorkerPreCommit1 {
					// get the market unsealed data, and copy to local
					if err := w.fetchUnseal(ctx, sealer, task); err != nil {
						return errRes(errors.As(err, w.workerCfg, len(task.ExtSizes)), &res)
					}
				}
			*/
			// checking is the cache in a different storage server, do fetch when it is.
			if len(task.WorkerID) > 0 && task.WorkerID != w.workerCfg.ID && task.Type > ffiwrapper.WorkerPledge && task.Type < ffiwrapper.WorkerCommit {
				// fetch the precommit cache data
				// lock bandwidth
				if err := api.RetryWorkerAddConn(ctx, task.WorkerID, 1); err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
			retryFetch:
				// fetch data
				uri := task.SectorStorage.WorkerInfo.SvcUri
				if err := w.fetchRemote(
					"http://"+uri,
					task.SectorStorage.SectorInfo.ID,
					sealer.RepoPath(),
					task.Type,
				); err != nil {
					log.Warnf("fileserver error, retry 10s later:%+s", err.Error())
					time.Sleep(10e9)
					goto retryFetch
				}
				// release bandwidth
				if err := api.RetryWorkerAddConn(ctx, task.WorkerID, -1); err != nil {
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
			}
		}
	}

	// lock the task to this worker
	if err := api.RetryWorkerLock(ctx, w.workerCfg.ID, task.Key(), "task in", int(task.Type)); err != nil {
		return errRes(errors.As(err, w.workerCfg), &res)
	}
	unlockWorker := false

	sector := storiface.SectorRef{
		ID:        task.SectorID,
		ProofType: task.ProofType,
		SectorFile: storiface.SectorFile{
			SectorId:       storiface.SectorName(task.SectorID),
			SealedRepo:     sealer.RepoPath(),
			UnsealedRepo:   sealer.RepoPath(),
			IsMarketSector: task.SectorStorage.UnsealedStorage.ID != 0,
		},
		StoreUnseal: task.StoreUnseal,
	}
	switch task.Type {
	case ffiwrapper.WorkerPledge:
		// fetch market deal
		tmpFile := ""
		defer func() {
			if len(tmpFile) > 0 {
				os.Remove(tmpFile)
			}
		}()
		// fetch staging file
		oriReaderKind := task.PieceData.ReaderKind
		if oriReaderKind == shared.PIECE_DATA_KIND_SERVER {
			stagingFile, err := w.FetchStaging(ctx, sealer, task)
			tmpFile = stagingFile
			if err != nil {
				return errRes(errors.As(err, task), &res)
			}
			// update to local mode
			task.PieceData.ReaderKind = shared.PIECE_DATA_KIND_FILE
			task.PieceData.LocalPath = tmpFile
		}
		rsp, err := sealer.ExecAddPiece(ctx, task)
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		if len(task.ExtSizes) > 0 {
			res.Pieces = rsp.Pieces
			if sector.IsMarketSector {
				// push unsealed, so the market addpiece can make a index for continue
			retry:
				log.Info("==================sector.StoreUnseal====================", sector.StoreUnseal)
				if err := w.pushUnsealed(ctx, sealer, task); err != nil {
					log.Warn(errors.As(err))
					time.Sleep(10e9)
					goto retry
				}
			}
		}else {
			res.Piece = rsp.Data
		}
		// checking is the next step interrupted
		unlockWorker = (w.workerCfg.ParallelPrecommit1 == 0)

	case ffiwrapper.WorkerPreCommit1:
		if _, err := os.Stat(filepath.Join(sealer.RepoPath(), "unsealed", task.SectorName())); err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, task), &res)
			}
			// create the garbage unsealed data
			pieces, err := sealer.PledgeSector(ctx, sector, task.ExistingPieceSizes, task.ExtSizes...)
			if err != nil {
				return errRes(errors.As(err, task), &res)
			}
			// TODO: checksum the pieces?
			if len(pieces) != len(task.Pieces) {
				log.Error(errors.New("piece length not match").As(task, pieces))
			} else {
				for i, p := range task.Pieces {
					if pieces[i].PieceCID.String() != p.PieceCID.String() {
						log.Error(errors.New("piece data not match").As(task, pieces[i], i))
					}
				}
			}
			task.Pieces = pieces
		}

	retryConfirmStaging:
		if err := w.ConfirmStaging(ctx, sealer, task.SectorName()); err != nil {
			log.Warn(errors.As(err))
			time.Sleep(10e9)
			goto retryConfirmStaging
		}
		rspco, err := sealer.ExecPreCommit1(ctx, task)
		res.PreCommit1Out = rspco
		if err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		// checking is the next step interrupted
		unlockWorker = (w.workerCfg.ParallelPrecommit2 == 0)
		utils.DeleteC1Out(sector)
	case ffiwrapper.WorkerPreCommit2:
		if task.Snap {
			if err := w.fetchUnseal(ctx, sealer, task); err != nil {
				return errRes(errors.As(err, w.workerCfg, len(task.ExtSizes)), &res)
			}
			if err := w.fetchSealed(ctx, sealer, task); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			log.Info("fetch cache, sealed files form remote storage")
			out, err := sealer.ReplicaUpdate(ctx, sector, task.Pieces)
			res.ReplicaUpdateOut = out
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		} else {
			//out, err := sealer.SealPreCommit2(ctx, sector, task.PreCommit1Out)
			out, err := sealer.ExecPreCommit2(ctx, task)
			res.PreCommit2Out = out
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			var commR = out.Sealed.String()
			var redoTimes int64
			var errCommR string
			switch w.ssize {
			case 64 * GB:
				errCommR = "bagboea4b5abcbybig6p4wwrozr7zdzj62afkq3kwjfb4ywla5hly7ir6wcivrg3p"
			case 32 * GB:
				errCommR = "bagboea4b5abcaefyn3i26gpj4odjnnceqc6uju4jfuzf5cw7zq3t3uw6welsdwyd"
			}
			for strings.EqualFold(commR, errCommR) && redoTimes < 2 {
				redoTimes++
				log.Infof("WARN###: Redo P2 : times %v ", redoTimes)
				out, err := sealer.ExecPreCommit2(ctx, task)
				res.PreCommit2Out = out
				if err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
				commR = out.Sealed.String()
			}
		}
	case ffiwrapper.WorkerCommit:
		pieceInfo := task.Pieces
		cids := &task.Cids
		//判断C1输出文件是否存在，如果存在，则跳过C1
		pathTxt := sector.CachePath() + "/c1.out"
		isExist, err := ffiwrapper.PathExists(pathTxt)
		if err != nil {
			log.Error("Read C1  PathExists Err :", err)
		}
		var c1Out []byte
		if isExist {
			c1Out, err = ioutil.ReadFile(pathTxt)
			if err != nil {
				log.Error("Read c1.out Err ", err)
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			log.Info(sector.CachePath() + " ==========c1 retry")
		} else {
			c1Out, err = sealer.SealCommit1(ctx, sector, task.SealTicket, task.SealSeed, pieceInfo, *cids)
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
			err = ffiwrapper.WriteSectorCC(pathTxt, c1Out)
			if err != nil {
				log.Error("=============================WriteSector C1 ===err: ", err)
			}
		}
		w.removeDataLayer(ctx, sector.CachePath(), false)
		localSectors.WriteMap(task.SectorName(), ffiwrapper.WorkerCommitDone)
		// if local gpu no set, using remotes .
		if w.workerCfg.ParallelCommit == 0 && !w.workerCfg.Commit2Srv {
			for {
				res.Commit2Out, err = CallCommit2Service(ctx, task, c1Out)
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
			res.Commit2Out, err = sealer.SealCommit2(ctx, sector, c1Out)
			if err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	// SPEC: cancel deal with worker finalize, because it will post failed when commit2 is online and finalize is interrupt.
	// SPEC: maybe it should failed on commit2 but can not failed on transfering the finalize data on windowpost.
	// TODO: when testing stable finalize retrying and reopen it.
	case ffiwrapper.WorkerFinalize:
		if task.Snap {
			updateFile := sealer.SectorPath("update", task.SectorName())
			_, err := os.Stat(string(updateFile))
			if err != nil {
				if !os.IsNotExist(err) {
					return errRes(errors.As(err, updateFile), &res)
				}
				// no file to finalize, just return done.
			} else {
				if err := sealer.FinalizeReplicaUpdate(ctx, sector); err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
				//push update updateCache 到远程存储
				log.Info("push update , updateCache to remote storage")
				if err := w.pushUpdateCache(ctx, sealer, task, false); err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
			}
		} else {
			//fix sector rebuild tool : disk space full
			w.removeDataLayer(ctx, sector.CachePath(), true)
			localSectors.WriteMap(task.SectorName(), ffiwrapper.WorkerFinalize)
			sealedFile := sealer.SectorPath("sealed", task.SectorName())
			_, err := os.Stat(string(sealedFile))
			if err != nil {
				if !os.IsNotExist(err) {
					return errRes(errors.As(err, sealedFile), &res)
				}
				// no file to finalize, just return done.
			} else {
				if err := w.pushCache(ctx, sealer, task, false); err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
				if err := sealer.FinalizeSector(ctx, sector); err != nil {
					return errRes(errors.As(err, w.workerCfg), &res)
				}
				if err := w.RemoveRepoSector(ctx, sealer.RepoPath(), task.SectorName()); err != nil {
					log.Warn(errors.As(err))
				}
			}
		}
	case ffiwrapper.WorkerUnseal:
		if err := sealer.UnsealPiece(ctx, sector, task.UnsealOffset, task.UnsealSize, task.SealRandomness, task.Commd); err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		if err := w.pushCache(ctx, sealer, task, true); err != nil {
			return errRes(errors.As(err, w.workerCfg), &res)
		}
		if err := w.RemoveRepoSector(ctx, sealer.RepoPath(), task.SectorName()); err != nil {
			log.Warn(errors.As(err))
		}
	case ffiwrapper.WorkerReplicaUpdate:
		ruOut, err := sealer.ReplicaUpdate(ctx, sector, task.Pieces)
		res.ReplicaUpdateOut = ruOut
		if err != nil {
			return errRes(errors.As(err, w.workerCfg, sector), &res)
		}
	case ffiwrapper.WorkerProveReplicaUpdate1:
		ru1Out, err := sealer.ProveReplicaUpdate1(ctx, sector, task.SectorKey, task.NewSealed, task.NewUnsealed)
		res.ReplicaVanillaProofs = ru1Out
		if err != nil {
			return errRes(errors.As(err, w.workerCfg, sector), &res)
		}
	case ffiwrapper.WorkerProveReplicaUpdate2:
		// if local gpu no set, using remotes .
		if w.workerCfg.ParallelCommit == 0 && !w.workerCfg.Commit2Srv {
			for {
				ru2Out, err := CallProveReplicaUpdate2Service(ctx, sector, task, task.VanillaProofs)
				res.ReplicaUpdateProof = ru2Out
				if err != nil {
					log.Warn(errors.As(err))
					time.Sleep(10e9)
					continue
				}
				break
			}
		}
		// call gpu service failed, using local instead.
		if len(res.ReplicaUpdateProof) == 0 {
			ru2Out, err := sealer.ProveReplicaUpdate2(ctx, sector, task.SectorKey, task.NewSealed, task.NewUnsealed, task.VanillaProofs)
			res.ReplicaUpdateProof = ru2Out
			if err != nil {
				return errRes(errors.As(err, w.workerCfg, sector), &res)
			}
		}
	case ffiwrapper.WorkerGenerateSectorKeyFromData:
		if err := sealer.GenerateSectorKeyFromData(ctx, sector, task.Commd); err != nil {
			return errRes(errors.As(err, w.workerCfg, sector), &res)
		}
	case ffiwrapper.WorkerFinalizeReplicaUpdate:
		updateFile := sealer.SectorPath("update", task.SectorName())
		_, err := os.Stat(updateFile)
		if err != nil {
			if !os.IsNotExist(err) {
				return errRes(errors.As(err, updateFile), &res)
			}
			// no file to finalize, just return done.
		} else {
			if err := sealer.FinalizeReplicaUpdate(ctx, sector); err != nil {
				return errRes(errors.As(err, w.workerCfg, sector), &res)
			}
			if err := w.pushUpdateCache(ctx, sealer, task, false); err != nil {
				return errRes(errors.As(err, w.workerCfg), &res)
			}
		}
	}

	// release the worker when stage is interrupted
	if unlockWorker {
		log.Info("Release Worker by:", task)
		if err := api.RetryWorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "transfer to another worker", database.SECTOR_STATE_MOVE); err != nil {
			log.Warn(errors.As(err))
			return errRes(errors.As(err, w.workerCfg), &res)
		}
	}
	return res
}
