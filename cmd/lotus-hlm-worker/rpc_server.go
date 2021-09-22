package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"

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
	go out.c2outClear()
	return out
}

type c2out struct {
	t time.Time
	r storage.Proof
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
	c2outs   sync.Map //p1->c2; c2 handing and p1 restart; c2->p1 失败 此时将结果缓存 等待p1重做c2的时候直接从缓存获取结果
}

func (w *rpcServer) c2outClear() {
	var (
		interval = time.Minute * 10
		expire   = time.Minute * 20
	)

	for {
		<-time.After(interval)

		w.c2outs.Range(func(k, v interface{}) bool {
			if out, ok := v.(*c2out); ok {
				if time.Since(out.t) > expire {
					w.c2outs.Delete(k)
				}
			} else {
				w.c2outs.Delete(k)
			}
			return true
		})
	}
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
	sid := w.sectorName(sector.SectorID)
	if obj, ok := w.c2outs.Load(sid); ok {
		if out, ok := obj.(*c2out); ok {
			log.Infof("SealCommit2 RPC hit-cache:%v, current c2sids: %v", sector, w.getC2sids())
			return out.r, nil
		}
	}

	w.c2sidsRW.Lock()
	if _, ok := w.c2sids[sid]; ok {
		w.c2sidsRW.Unlock()
		log.Infof("SealCommit2 RPC dumplicate:%v, current c2sids: %v", sector, w.getC2sids())
		return storage.Proof{}, errors.New("sector is sealing").As(sid)
	}
	w.c2sids[sid] = sector.SectorID
	w.c2sidsRW.Unlock()

	log.Infof("SealCommit2 RPC in:%v, current c2sids: %v", sector, w.getC2sids())
	defer func() {
		w.c2sidsRW.Lock()
		delete(w.c2sids, sid)
		w.c2sidsRW.Unlock()
		log.Infof("SealCommit2 RPC out:%v, current c2sids: %v", sector, w.getC2sids())

		//只能在c2 worker执行UnlockGPUService
		//不能在p1 worker调用c2完成后执行：会出现p1 worker重启而没执行到这句 造成miner那边维护的c2 worker的busy一直不正确
		napi, _ := GetNodeApi()
		if err := napi.RetryUnlockGPUService(ctx, w.workerID, sector.TaskKey); err != nil {
			log.Errorf("SealCommit2 unlock gpu service error: %v", err)
		}
	}()

	out, err := w.sb.SealCommit2(ctx, storage.SectorRef{ID: sector.SectorID, ProofType: sector.ProofType}, commit1Out)
	if err == nil {
		w.c2outs.Store(sid, &c2out{r: out, t: time.Now()})
	}
	return out, err
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
