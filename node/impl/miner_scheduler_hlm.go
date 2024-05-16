package impl

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/gwaylib/errors"
)

type HlmMinerScheduler struct {
	sm *StorageMinerAPI
}

func NewHlmMinerScheduler(sm *StorageMinerAPI) *HlmMinerScheduler {
	return &HlmMinerScheduler{sm: sm}
}

type jwtPayload struct {
	Allow []auth.Permission
}

func (hs *HlmMinerScheduler) Version(context.Context) (api.APIVersion, error) {
	v, err := api.VersionForType(api.RunningNodeType)
	if err != nil {
		return api.APIVersion{}, err
	}

	return api.APIVersion{
		Version:    build.UserVersion(),
		APIVersion: v,

		BlockDelay: build.BlockDelaySecs,
	}, nil
}

func (hs *HlmMinerScheduler) AuthVerify(ctx context.Context, token string) ([]auth.Permission, error) {
	var payload jwtPayload
	if _, err := jwt.Verify([]byte(token), (*jwt.HMACSHA)(hs.sm.WorkerAPISecret), &payload); err != nil {
		log.Warnf("error token %+v", token)
		return nil, errors.As(err)
	}

	return payload.Allow, nil
}

func (hs *HlmMinerScheduler) AuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error) {
	p := jwtPayload{
		Allow: perms, // TODO: consider checking validity
	}

	return jwt.Sign(&p, (*jwt.HMACSHA)(hs.sm.WorkerAPISecret))
}

func (hs *HlmMinerScheduler) ActorAddress(context.Context) (address.Address, error) {
	return hs.sm.Miner.Address(), nil
}

func (hs *HlmMinerScheduler) WorkerAddress(ctx context.Context, act address.Address, task types.TipSetKey) (address.Address, error) {
	mInfo, err := hs.sm.Full.StateMinerInfo(ctx, act, task)
	if err != nil {
		return address.Address{}, err
	}
	return mInfo.Worker, nil
}
func (hs *HlmMinerScheduler) ActorSectorSize(ctx context.Context, addr address.Address) (abi.SectorSize, error) {
	mi, err := hs.sm.Full.StateMinerInfo(ctx, addr, types.EmptyTSK)
	if err != nil {
		return 0, err
	}
	return mi.SectorSize, nil
}

func (hs *HlmMinerScheduler) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*ffiwrapper.Commit2Worker, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).SelectCommit2Service(ctx, sector)
}
func (hs *HlmMinerScheduler) UnlockGPUService(ctx context.Context, rst *ffiwrapper.Commit2Result) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).UnlockGPUService(ctx, rst)
}
func (hs *HlmMinerScheduler) WorkerQueue(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AddWorker(ctx, cfg)
}
func (hs *HlmMinerScheduler) WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskDone(ctx, res)
}

func (hs *HlmMinerScheduler) WorkerFileWatch(ctx context.Context, res ffiwrapper.WorkerCfg) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).WorkerFileWatch(ctx, res)
}

func (hs *HlmMinerScheduler) WorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskWorkingById(sid)
}

func (hs *HlmMinerScheduler) WorkerAddConn(ctx context.Context, wid string, num int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AddWorkerConn(wid, num)
}

func (hs *HlmMinerScheduler) WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).PrepareWorkerConn()
}
func (hs *HlmMinerScheduler) WorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).PrepareWorkerConnV1(skipWid)
}
func (hs *HlmMinerScheduler) WorkerMinerConn(ctx context.Context) (int, error) {
	return fileserver.Conns(), nil
}
func (hs *HlmMinerScheduler) WorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).LockWorker(ctx, workerId, taskKey, memo, sectorState)
}
func (hs *HlmMinerScheduler) WorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).UnlockWorker(ctx, workerId, taskKey, memo, sectorState)
}

func (hs *HlmMinerScheduler) ChecksumStorage(ctx context.Context, ver int64) ([]database.StorageInfo, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).ChecksumStorage(ver)
}
func (sm *HlmMinerScheduler) NewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error) {
	info, err := database.GetStorage(id)
	if err != nil {
		return "", errors.As(err)
	}
	token, err := hlmclient.NewAuthClient(info.MountAuthUri, info.MountAuth).NewFileToken(ctx, sid)
	if err != nil {
		return "", errors.As(err, id, sid)
	}
	return string(token), nil
}
func (hs *HlmMinerScheduler) DelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error {
	info, err := database.GetStorage(id)
	if err != nil {
		return errors.As(err, id)
	}
	if _, err := hlmclient.NewAuthClient(info.MountAuthUri, info.MountAuth).DeleteFileToken(ctx, sid); err != nil {
		return errors.As(err, id, sid)
	}
	return nil
}
func (hs *HlmMinerScheduler) PreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).PreStorageNode(sectorId, clientIp, kind)
}
func (hs *HlmMinerScheduler) CommitStorageNode(ctx context.Context, sectorId string, kind int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).CommitStorageNode(sectorId, kind)
}
func (hs *HlmMinerScheduler) CancelStorageNode(ctx context.Context, sectorId string, kind int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).CancelStorageNode(sectorId, kind)
}
func (hs *HlmMinerScheduler) AcquireStorageConnCount(ctx context.Context, sectorId string, kind int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AcquireStorageConnCount(sectorId, kind)
}
func (hs *HlmMinerScheduler) ReleaseStorageConnCount(ctx context.Context, sectorId string, kind int) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).ReleaseStorageConnCount(sectorId, kind)
}
func (hs *HlmMinerScheduler) HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).HlmSectorGetState(sid)
}
func (hs *HlmMinerScheduler) GetWorkerBusyTask(ctx context.Context, wid string) (int, error) {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).GetWorkerBusyTask(ctx, wid)
}
func (hs *HlmMinerScheduler) RequestDisableWorker(ctx context.Context, wid string) error {
	return hs.sm.StorageMgr.Prover.(*ffiwrapper.Sealer).RequestDisableWorker(ctx, wid)
}
func (hs *HlmMinerScheduler) GetMinerInfo(ctx context.Context) string {
	return hs.sm.Miner.Maddr()
}
func (hs *HlmMinerScheduler) PutStatisSeal(ctx context.Context, st database.StatisSeal) error {
	return errors.As(database.PutStatisSeal(&st))
}

func (hs *HlmMinerScheduler) GetStorage(ctx context.Context, storageId int64) (*database.StorageInfo, error) {
	return database.GetStorageInfo(storageId)
}
func (hs *HlmMinerScheduler) GetMarketDealInfo(ctx context.Context, propID string) (*database.MarketDealInfo, error) {
	m, err := database.GetMarketDealInfo(propID)
	if err != nil {
		return nil, errors.As(err)
	}
	return m, nil
}
func (hs *HlmMinerScheduler) GetMarketDealInfoBySid(ctx context.Context, sid string) ([]database.MarketDealInfo, error) {
	m, err := database.GetMarketDealInfoBySid(sid)
	if err != nil {
		return nil, errors.As(err)
	}
	return m, nil
}

// for build testing
var _ api.HlmMinerSchedulerAPI = &HlmMinerScheduler{}
