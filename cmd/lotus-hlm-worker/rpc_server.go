package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"os"
	"path/filepath"
	"sync"

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
	return fmt.Sprintf("s-t0%d-%d", sid.Miner, sid.Number)
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

func (w *rpcServer) SealCommit2(ctx context.Context, sector api.SectorRef, commit1Out storage.Commit1Out) (storage.Proof, error) {
	var (
		dump = false //是否重复扇区
		prf  storage.Proof
		sid  = w.sectorName(sector.SectorID)
		out  = &ffiwrapper.Commit2Result{
			WorkerId: w.workerID,
			TaskKey:  sector.TaskKey,
			Sid:      sid,
		}
	)

	log.Infof("SealCommit2 RPC in:%v, current c2sids: %v", sector, w.getC2sids())
	defer func() {
		if !dump {
			w.c2sidsRW.Lock()
			delete(w.c2sids, sid)
			w.c2sidsRW.Unlock()
		}

		log.Infof("SealCommit2 RPC out:%v, current c2sids: %v, error: %v", sector, w.getC2sids(), out.Err)
		//只能在c2 worker执行UnlockGPUService
		//不能在p1 worker调用c2完成后执行：会出现p1 worker重启而没执行到这句 造成miner那边维护的c2 worker的busy一直不正确
		mApi, _ := GetNodeApi()
		if err := mApi.RetryUnlockGPUService(ctx, out); err != nil {
			log.Errorf("SealCommit2 unlock gpu service error: %v", err)
		}
	}()

	w.c2sidsRW.Lock()
	if _, ok := w.c2sids[sid]; ok {
		dump = true
		w.c2sidsRW.Unlock()
		log.Infof("SealCommit2 RPC dumplicate:%v, current c2sids: %v", sector, w.getC2sids())
		out.Err = errors.New("sector is sealing").As(sid)
		return storage.Proof{}, out.Err
	}
	w.c2sids[sid] = sector.SectorID
	w.c2sidsRW.Unlock()

	if prf, out.Err = w.sb.SealCommit2(ctx, storage.SectorRef{ID: sector.SectorID, ProofType: sector.ProofType}, commit1Out); out.Err == nil {
		out.Proof = hex.EncodeToString(prf)
	}
	return prf, out.Err
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
		if err := database.Mount(
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

func (w *rpcServer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
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
func (w *rpcServer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) (api.WindowPoStResp, error) {
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
	log.Infof("GenerateWindowPoSt: %+v ", sectorInfo)
	proofs, ignore, err := w.sb.GenerateWindowPoSt(ctx, minerID, sectorInfo, randomness)
	if err != nil {
		log.Warnf("ignore len:%d", len(ignore))
	}
	return api.WindowPoStResp{
		Proofs: proofs,
		Ignore: ignore,
	}, err
}
