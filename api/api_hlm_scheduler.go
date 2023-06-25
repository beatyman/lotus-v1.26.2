package api

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
)

type HlmMinerSchedulerAPI interface {
	Version(context.Context) (APIVersion, error)

	// worker api need.
	AuthVerify(ctx context.Context, token string) ([]auth.Permission, error)
	AuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error)

	ActorAddress(context.Context) (address.Address, error)
	WorkerAddress(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	ActorSectorSize(context.Context, address.Address) (abi.SectorSize, error)

	SelectCommit2Service(context.Context, abi.SectorID) (*ffiwrapper.Commit2Worker, error)
	UnlockGPUService(ctx context.Context, rst *ffiwrapper.Commit2Result) error

	WorkerQueue(context.Context, ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error)
	WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error

	WorkerFileWatch(ctx context.Context, res ffiwrapper.WorkerCfg) error

	WorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error)

	WorkerAddConn(ctx context.Context, wid string, num int) error
	WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error)
	WorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error)
	WorkerMinerConn(ctx context.Context) (int, error)

	WorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error
	WorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error

	ChecksumStorage(ctx context.Context, ver int64) ([]database.StorageInfo, error)
	NewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error)
	DelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error
	PreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error)
	CommitStorageNode(ctx context.Context, sectorId string, kind int) error
	CancelStorageNode(ctx context.Context, sectorId string, kind int) error
	HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error)
	GetWorkerBusyTask(ctx context.Context, wid string) (int, error)
	RequestDisableWorker(ctx context.Context, wid string) error
	GetMinerInfo(ctx context.Context) string
	GetStorage(ctx context.Context, storageId int64) (*database.StorageInfo, error)
	PutStatisSeal(ctx context.Context, st database.StatisSeal) error
}