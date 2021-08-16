package impl

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/node/modules/proxy"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

func (sm *StorageMinerAPI) Testing(ctx context.Context, fnName string, args []string) error {
	return sm.Miner.Testing(ctx, fnName, args)
}
func (sm *StorageMinerAPI) StatisWin(ctx context.Context, id string) (*database.StatisWin, error) {
	return database.GetStatisWin(id)
}
func (sm *StorageMinerAPI) StatisWins(ctx context.Context, now time.Time, limit int) ([]database.StatisWin, error) {
	return database.GetStatisWins(now, limit)
}

func (sm *StorageMinerAPI) ProxyAutoSelect(ctx context.Context, on bool) error {
	// save to db
	b := []byte{0}
	if on {
		b[0] = 1
	}
	if err := sm.DS.Put(proxy.PROXY_AUTO, b); err != nil {
		return errors.As(err)
	}
	return proxy.SetLotusAutoSelect(on)
}
func (sm *StorageMinerAPI) ProxyChange(ctx context.Context, idx int) error {
	return proxy.LotusProxyChange(idx)
}
func (sm *StorageMinerAPI) ProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	return proxy.LotusProxyStatus(ctx, cond)
}
func (sm *StorageMinerAPI) ProxyReload(ctx context.Context) error {
	return proxy.RealoadLotusProxy(ctx)
}

func (sm *StorageMinerAPI) WdpostEnablePartitionSeparate(ctx context.Context, enable bool) error {
	return sm.Miner.WdpostEnablePartitionSeparate(enable)
}
func (sm *StorageMinerAPI) WdpostSetPartitionNumber(ctx context.Context, number int) error {
	return sm.Miner.WdpostSetPartitionNumber(number)
}
func (sm *StorageMinerAPI) WdPostGetLog(ctx context.Context, index uint64) ([]api.WdPoStLog, error) {
	return sm.Miner.GetWdPoStLog(ctx, index)
}

func (sm *StorageMinerAPI) RunPledgeSector(ctx context.Context) error {
	return sm.Miner.RunPledgeSector()
}
func (sm *StorageMinerAPI) StatusPledgeSector(ctx context.Context) (int, error) {
	return sm.Miner.StatusPledgeSector()
}
func (sm *StorageMinerAPI) StopPledgeSector(ctx context.Context) error {
	return sm.Miner.ExitPledgeSector()
}
func (sm *StorageMinerAPI) RebuildPledgeSector(ctx context.Context, sid string, storage uint64) error {
	return sm.Miner.RebuildSector(context.TODO(), sid, storage)
}

func (sm *StorageMinerAPI) HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	return database.GetSectorInfo(sid)
}
func (sm *StorageMinerAPI) HlmSectorSetState(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).UpdateSectorState(sid, memo, state, force, reset)
}

// Message communication
func (sm *StorageMinerAPI) HlmSectorListAll(ctx context.Context) ([]api.SectorInfo, error) {
	sectors, err := sm.Miner.ListSectors()
	if err != nil {
		return nil, err
	}

	out := []api.SectorInfo{}
	for _, sector := range sectors {
		out = append(out, api.SectorInfo{
			State:    api.SectorState(sector.State),
			SectorID: sector.SectorNumber,
			// TODO: more?
		})
	}
	return out, nil
}
func (sm *StorageMinerAPI) HlmSectorFile(ctx context.Context, sid string) (*storage.SectorFile, error) {
	repo := sm.StorageMgr.Prover.(*ffiwrapper.Sealer).RepoPath()
	return database.GetSectorFile(sid, repo)
}
func (sm *StorageMinerAPI) HlmSectorCheck(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error) {
	maddr, err := sm.ActorAddress(ctx)
	if err != nil {
		return 0, err
	}
	mi, err := sm.Full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return 0, err
	}

	repo := sm.StorageMgr.Prover.(*ffiwrapper.Sealer).RepoPath()
	file, err := database.GetSectorFile(sid, repo)
	if err != nil {
		return 0, errors.As(err)
	}
	id, err := storage.ParseSectorID(sid)
	all, _, _, err := ffiwrapper.CheckProvable(ctx, []storage.SectorRef{
		storage.SectorRef{
			ID:         id,
			ProofType:  abi.RegisteredSealProof(mi.WindowPoStProofType),
			SectorFile: *file,
		},
	}, nil, timeout)
	if err != nil {
		return 0, errors.As(err)
	}
	if len(all) != 1 {
		return 0, errors.New("unexpect return").As(sid, timeout, len(all))
	}
	if all[0].Err != nil {
		return 0, errors.As(all[0].Err)
	}
	return all[0].Used, nil
}

func (sm *StorageMinerAPI) WorkerAddress(ctx context.Context, act address.Address, task types.TipSetKey) (address.Address, error) {
	mInfo, err := sm.Full.StateMinerInfo(ctx, act, task)
	if err != nil {
		return address.Address{}, err
	}
	return mInfo.Worker, nil
}

func (sm *StorageMinerAPI) PauseSeal(ctx context.Context, pause int32) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).PauseSeal(ctx, pause)
}
func (sm *StorageMinerAPI) WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).WorkerStats(), nil
}
func (sm *StorageMinerAPI) WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).WorkerRemoteStats()
}

func (sm *StorageMinerAPI) WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskWorking(workerId)
}
func (sm *StorageMinerAPI) WorkerGcLock(ctx context.Context, workerId string) ([]string, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).GcWorker(workerId)
}

func (sm *StorageMinerAPI) WorkerInfo(ctx context.Context, wid string) (*database.WorkerInfo, error) {
	return database.GetWorkerInfo(wid)
}
func (sm *StorageMinerAPI) WorkerSearch(ctx context.Context, ip string) ([]database.WorkerInfo, error) {
	return database.SearchWorkerInfo(ip)
}
func (sm *StorageMinerAPI) WorkerDisable(ctx context.Context, wid string, disable bool) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).DisableWorker(ctx, wid, disable)
}

func (sm *StorageMinerAPI) VerHLMStorage(ctx context.Context) (int64, error) {
	return database.StorageMaxVer()
}
func (sm *StorageMinerAPI) GetHLMStorage(ctx context.Context, id int64) (*database.StorageInfo, error) {
	info, err := database.GetStorageInfo(id)
	if err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}
func (sm *StorageMinerAPI) SearchHLMStorage(ctx context.Context, ip string) ([]database.StorageInfo, error) {
	return database.SearchStorageInfoBySignalIp(ip)
}
func (sm *StorageMinerAPI) AddHLMStorage(ctx context.Context, info *database.StorageAuth) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AddStorage(ctx, info)
}
func (sm *StorageMinerAPI) DisableHLMStorage(ctx context.Context, id int64, disable bool) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).DisableStorage(ctx, id, disable)
}
func (sm *StorageMinerAPI) MountHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).MountStorage(ctx, id)
}

func (sm *StorageMinerAPI) RelinkHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).RelinkStorage(ctx, id)
}
func (sm *StorageMinerAPI) ReplaceHLMStorage(ctx context.Context, info *database.StorageAuth) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).ReplaceStorage(ctx, info)
}
func (sm *StorageMinerAPI) ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).ScaleStorage(ctx, id, size, work)
}
func (sm *StorageMinerAPI) StatusHLMStorage(ctx context.Context, storageId int64, timeout time.Duration) ([]database.StorageStatus, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).StorageStatus(ctx, storageId, timeout)
}
func (c *StorageMinerAPI) GetProvingCheckTimeout(ctx context.Context) (time.Duration, error) {
	return build.GetProvingCheckTimeout(), nil
}
func (c *StorageMinerAPI) SetProvingCheckTimeout(ctx context.Context, timeout time.Duration) error {
	build.SetProvingCheckTimeout(timeout)
	return nil
}
func (c *StorageMinerAPI) GetFaultCheckTimeout(ctx context.Context) (time.Duration, error) {
	return build.GetFaultCheckTimeout(), nil
}
func (c *StorageMinerAPI) SetFaultCheckTimeout(ctx context.Context, timeout time.Duration) error {
	build.SetFaultCheckTimeout(timeout)
	return nil
}