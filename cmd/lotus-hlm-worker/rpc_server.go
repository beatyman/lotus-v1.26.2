package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"os"

	"github.com/gwaylib/errors"
)

func newRpcServer(id string, repo string, sb *ffiwrapper.Sealer) *rpcServer {
	out := &rpcServer{
		workerID:  id,
		minerRepo: repo,
		sb:        sb,

		storageCache: map[int64]database.StorageInfo{},
		c2sids:       make(map[string]abi.SectorID),
	}
	return out
}

type rpcServer struct {
	workerID  string
	minerRepo string
	sb        *ffiwrapper.Sealer

	storageLk    sync.Mutex
	storageVer   int64
	storageCache map[int64]database.StorageInfo

	c2sidsRW sync.RWMutex
	c2sids   map[string]abi.SectorID
}

func (w *rpcServer) sectorName(sid abi.SectorID) string {
	return storiface.SectorName(sid)
}

func (w *rpcServer) getC2sids() []abi.SectorID {
	w.c2sidsRW.RLock()
	defer w.c2sidsRW.RUnlock()

	out := make([]abi.SectorID, 0, 0)
	for _, sid := range w.c2sids {
		out = append(out, sid)
	}
	return out
}

func (w *rpcServer) Version(context.Context) (string, error) {
	return "", nil
}

func (w *rpcServer) SealCommit2(ctx context.Context, sector api.SectorRef, commit1Out storiface.Commit1Out) (storiface.Proof, error) {
	var (
		err  error
		dump = false //是否重复扇区(重复:正在执行中...)
		prf  storiface.Proof
		sid  = w.sectorName(sector.SectorID)
		out  = &ffiwrapper.Commit2Result{
			WorkerId: w.workerID,
			TaskKey:  sector.TaskKey,
			Sid:      sid,
		}
	)

	log.Infof("SealCommit2 RPC in:%v, current c2sids: %v", sector, w.getC2sids())
	beginTime := time.Now()
	defer func() {
		log.Infof("SealCommit2 RPC out:%v, current c2sids: %v, error: %v", sector, w.getC2sids(), out.Err)
		if dump { //正在执行中的任务 不能释放锁
			return
		}

		//p1->c2如果断线 会每10s重试一次 所以不能在做完任务后马上删除 10s后p1就可以从miner获取到缓存的结果了
		go func() {
			<-time.After(time.Second * 11)
			w.c2sidsRW.Lock()
			delete(w.c2sids, sid)
			w.c2sidsRW.Unlock()
		}()

		//只能在c2 worker执行UnlockGPUService
		//不能在p1 worker调用c2完成后执行：会出现p1 worker重启而没执行到这句 造成miner那边维护的c2 worker的busy一直不正确
		mApi, _ := GetNodeApi()
		if err := mApi.RetryUnlockGPUService(ctx, out); err != nil {
			log.Errorf("SealCommit2 unlock gpu service error: %v", err)
		}

		endTime := time.Now()
		statErr := "success"
		if len(out.Err) > 0 {
			statErr = out.Err
		}
		if err := mApi.PutStatisSeal(ctx, database.StatisSeal{
			TaskID:    fmt.Sprintf("%s_41", sid),
			Sid:       sid,
			Stage:     database.SEAL_STAGE_C2,
			WorkerID:  w.workerID,
			BeginTime: beginTime,
			EndTime:   endTime,
			Used:      int64(endTime.Sub(beginTime).Seconds()),
			Error:     statErr,
		}); err != nil {
			log.Error(err)
		}

	}()

	w.c2sidsRW.Lock()
	if _, ok := w.c2sids[sid]; ok {
		dump = true
		w.c2sidsRW.Unlock()
		log.Infof("SealCommit2 RPC dumplicate:%v, current c2sids: %v", sector, w.getC2sids())
		err = errors.New("sector is sealing").As(sid)
		out.Err = err.Error()
		return storiface.Proof{}, err
	}
	w.c2sids[sid] = sector.SectorID
	w.c2sidsRW.Unlock()

	if prf, err = w.sb.SealCommit2(ctx, storiface.SectorRef{ID: sector.SectorID, ProofType: sector.ProofType}, commit1Out); err == nil {
		out.Proof = hex.EncodeToString(prf)
	} else {
		out.Err = err.Error()
	}
	return prf, err
}

func (w *rpcServer) loadMinerStorage(ctx context.Context, napi *api.RetryHlmMinerSchedulerAPI) error {
	up := os.Getenv("US3")
	if up != "" {
		return nil
	}
	if err := database.LockMount(w.minerRepo); err != nil {
		log.Infof("mount lock failed, skip mount the storages:%s", errors.As(err, w.minerRepo).Code())
		return nil
	}

	w.storageLk.Lock()
	defer w.storageLk.Unlock()
	// checksum
	list, err := napi.RetryChecksumStorage(ctx, w.storageVer)
	if err != nil {
		return errors.As(err)
	}
	// no storage to mount
	if len(list) == 0 {
		return nil
	}

	maxVer := int64(0)
	// mount storage data
	for _, info := range list {
		if maxVer < info.Version {
			maxVer = info.Version
		}
		cacheInfo, ok := w.storageCache[info.ID]
		if ok && cacheInfo.Version == info.Version {
			continue
		}

		if cacheInfo.Kind != database.STORAGE_KIND_SEALED {
			continue
		}
		// version not match
		if err := database.MountPostWorker(
			ctx,
			info.MountType,
			info.MountSignalUri,
			filepath.Join(info.MountDir, fmt.Sprintf("%d", info.ID)),
			info.MountOpt,
		); err != nil {
			return errors.As(err)
		}
		w.storageCache[info.ID] = info
	}
	w.storageVer = maxVer

	return nil
}

func (w *rpcServer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storiface.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	log.Infof("GenerateWinningPoSt RPC in:%d", minerID)
	defer log.Infof("GenerateWinningPoSt RPC out:%d", minerID)
	napi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}
	// load miner storage if not exist
	if err := w.loadMinerStorage(ctx, napi); err != nil {
		return nil, errors.As(err)
	}

	return w.sb.GenerateWinningPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *rpcServer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, poStProofType abi.RegisteredPoStProof, sectorInfo []storiface.ProofSectorInfo, randomness abi.PoStRandomness) (api.WindowPoStResp, error) {
	log.Infof("GenerateWindowPoSt RPC in:%d", minerID)
	defer log.Infof("GenerateWindowPoSt RPC out:%d", minerID)
	napi, err := GetNodeApi()
	if err != nil {
		return api.WindowPoStResp{}, errors.As(err)
	}
	// load miner storage if not exist
	if err := w.loadMinerStorage(ctx, napi); err != nil {
		return api.WindowPoStResp{}, errors.As(err)
	}
	proofs, ignore, err := w.sb.GenerateWindowPoSt(ctx, minerID, poStProofType, sectorInfo, randomness)
	if err != nil {
		log.Warnf("ignore len:%d", len(ignore))
	}
	return api.WindowPoStResp{
		Proofs: proofs,
		Ignore: ignore,
	}, err
}

func (w *rpcServer) ProveReplicaUpdate2(ctx context.Context, sector api.SectorRef, vanillaProofs storiface.ReplicaVanillaProofs) (storiface.ReplicaUpdateProof, error) {
	var (
		err                error
		dump               = false //是否重复扇区(重复:正在执行中...)
		replicaUpdateProof storiface.ReplicaUpdateProof
		sid                = w.sectorName(sector.SectorID)
		out                = &ffiwrapper.Commit2Result{
			WorkerId: w.workerID,
			TaskKey:  sector.TaskKey,
			Sid:      sid,
			Snap:     true,
		}
	)

	beginTime := time.Now()
	log.Infof("ProveReplicaUpdate2 RPC in:%v, current c2sids: %v", sector, w.getC2sids())
	defer func() {
		log.Infof("ProveReplicaUpdate2 RPC out:%v, current c2sids: %v, error: %v", sector, w.getC2sids(), out.Err)
		if dump { //正在执行中的任务 不能释放锁
			return
		}

		//p1->c2如果断线 会每10s重试一次 所以不能在做完任务后马上删除 10s后p1就可以从miner获取到缓存的结果了
		go func() {
			<-time.After(time.Second * 11)
			w.c2sidsRW.Lock()
			delete(w.c2sids, sid)
			w.c2sidsRW.Unlock()
		}()

		//只能在c2 worker执行UnlockGPUService
		//不能在p1 worker调用c2完成后执行：会出现p1 worker重启而没执行到这句 造成miner那边维护的c2 worker的busy一直不正确
		mApi, _ := GetNodeApi()
		if err := mApi.RetryUnlockGPUService(ctx, out); err != nil {
			log.Errorf("ProveReplicaUpdate2 unlock gpu service error: %v", err)
		}

		endTime := time.Now()
		statErr := "success"
		if len(out.Err) > 0 {
			statErr = out.Err
		}
		if err := mApi.PutStatisSeal(ctx, database.StatisSeal{
			TaskID:    fmt.Sprintf("%s_41", sid),
			Sid:       sid,
			Stage:     database.SEAL_STAGE_C2,
			WorkerID:  w.workerID,
			BeginTime: beginTime,
			EndTime:   endTime,
			Used:      int64(endTime.Sub(beginTime).Seconds()),
			Error:     statErr,
		}); err != nil {
			log.Error(err)
		}
	}()

	w.c2sidsRW.Lock()
	if _, ok := w.c2sids[sid]; ok {
		dump = true
		w.c2sidsRW.Unlock()
		log.Infof("ProveReplicaUpdate2 RPC dumplicate:%v, current c2sids: %v", sector, w.getC2sids())
		err = errors.New("sector is sealing").As(sid)
		out.Err = err.Error()
		return storiface.ReplicaUpdateProof{}, err
	}
	w.c2sids[sid] = sector.SectorID
	w.c2sidsRW.Unlock()
	sectorRef := storiface.SectorRef{
		ID:        sector.SectorID,
		ProofType: sector.ProofType,
		SectorFile: storiface.SectorFile{
			SectorId:     storiface.SectorName(sector.SectorID),
			SealedRepo:   w.sb.RepoPath(),
			UnsealedRepo: w.sb.RepoPath(),
		},
	}
	if replicaUpdateProof, err = w.sb.ProveReplicaUpdate2(ctx, sectorRef, sector.SectorKey, sector.NewSealed, sector.NewUnsealed, vanillaProofs); err == nil {
		out.Proof = hex.EncodeToString(replicaUpdateProof)
	} else {
		log.Error(err)
		//out.Err = err.Error()
	}
	return replicaUpdateProof, err
}
