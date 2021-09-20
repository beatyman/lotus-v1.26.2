package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/gwaylib/errors"
)

type rpcServer struct {
	workerID  string
	minerRepo string
	sb        *ffiwrapper.Sealer

	storageLk    sync.Mutex
	storageVer   int64
	storageCache map[int64]database.StorageInfo

	c2Lk    sync.Mutex
	c2Cache map[string]bool
}

func (w *rpcServer) Version(context.Context) (string, error) {
	return "", nil
}

func (w *rpcServer) SealCommit2(ctx context.Context, sector api.SectorRef, commit1Out storage.Commit1Out) (storage.Proof, error) {
	log.Infof("SealCommit2 RPC in:%d", sector)
	defer log.Infof("SealCommit2 RPC out:%d", sector)

	defer func() {
		//只能在c2 worker执行UnlockGPUService（不能在p1 worker调用c2完成后执行：会出现p1 worker重启而没执行到这句 造成miner那边维护的c2 worker的busy一直不正确）
		napi, _ := GetNodeApi()
		if err := napi.RetryUnlockGPUService(ctx, w.workerID, sector.TaskKey); err != nil {
			log.Warn(errors.As(err))
		}
	}()

	// remove the dumplicate request.
	w.c2Lk.Lock()
	if w.c2Cache == nil {
		w.c2Cache = map[string]bool{}
	}
	key := fmt.Sprintf("s-t0%d-%d", sector.Miner, sector.Number)
	_, ok := w.c2Cache[key]
	if ok {
		w.c2Lk.Unlock()
		return storage.Proof{}, errors.New("sector is sealing").As(key)
	}
	w.c2Cache[key] = true
	w.c2Lk.Unlock()

	defer func() {
		w.c2Lk.Lock()
		delete(w.c2Cache, key)
		w.c2Lk.Unlock()
	}()

	return w.sb.SealCommit2(ctx, storage.SectorRef{ID: sector.SectorID, ProofType: sector.ProofType}, commit1Out)
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
