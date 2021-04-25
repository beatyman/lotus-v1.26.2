package api

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
)

type HlmMinerProxy interface {
	// implement the proxy
	ProxyAutoSelect(context.Context, bool) error
	ProxyChange(context.Context, int) error
	ProxyReload(context.Context) error
	ProxyStatus(context.Context, ProxyStatCondition) (*ProxyStatus, error)
}

type HlmMinerProving interface {
	Testing(ctx context.Context, fnName string, args []string) error
	WdpostEnablePartitionSeparate(ctx context.Context, enable bool) error
	WdpostSetPartitionNumber(ctx context.Context, number int) error
}

type HlmMinerSector interface {
	RunPledgeSector(context.Context) error
	StatusPledgeSector(context.Context) (int, error)
	StopPledgeSector(context.Context) error

	HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error)
	HlmSectorSetState(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error)
	HlmSectorListAll(context.Context) ([]SectorInfo, error)
	HlmSectorFile(ctx context.Context, sid string) (*storage.SectorFile, error)
	HlmSectorCheck(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error)
}

type HlmMinerStorage interface {
	// for miner
	StatusMinerStorage(ctx context.Context) ([]byte, error)

	// for storage nodes
	VerHLMStorage(ctx context.Context) (int64, error)
	GetHLMStorage(ctx context.Context, id int64) (*database.StorageInfo, error)
	SearchHLMStorage(ctx context.Context, ip string) ([]database.StorageInfo, error)
	AddHLMStorage(ctx context.Context, info *database.StorageAuth) error
	DisableHLMStorage(ctx context.Context, id int64, disable bool) error
	MountHLMStorage(ctx context.Context, id int64) error
	RelinkHLMStorage(ctx context.Context, id int64) error
	ReplaceHLMStorage(ctx context.Context, info *database.StorageAuth) error
	ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error
	StatusHLMStorage(ctx context.Context, id int64, timeout time.Duration) ([]database.StorageStatus, error)
	NewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error)
	DelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error
	PreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error)
	CommitStorageNode(ctx context.Context, sectorId string, kind int) error
	CancelStorageNode(ctx context.Context, sectorId string, kind int) error
	ChecksumStorage(ctx context.Context, ver int64) ([]database.StorageInfo, error)
	GetProvingCheckTimeout(ctx context.Context) (time.Duration, error)
	SetProvingCheckTimeout(ctx context.Context, timeout time.Duration) error
	GetFaultCheckTimeout(ctx context.Context) (time.Duration, error)
	SetFaultCheckTimeout(ctx context.Context, timeout time.Duration) error
}

type HlmMinerWorker interface {
	// implements by hlm
	SelectCommit2Service(context.Context, abi.SectorID) (*ffiwrapper.WorkerCfg, error)
	UnlockGPUService(ctx context.Context, workerId, taskKey string) error
	PauseSeal(ctx context.Context, pause int32) error
	WorkerAddress(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error)
	WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error)
	WorkerQueue(context.Context, ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error)
	WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error)
	WorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error)
	WorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error
	WorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error
	WorkerGcLock(ctx context.Context, workerId string) ([]string, error)
	WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error
	WorkerInfo(ctx context.Context, wid string) (*database.WorkerInfo, error)
	WorkerSearch(ctx context.Context, ip string) ([]database.WorkerInfo, error)
	WorkerDisable(ctx context.Context, wid string, disable bool) error
	WorkerAddConn(ctx context.Context, wid string, num int) error
	WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error)
	WorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error)
	WorkerMinerConn(ctx context.Context) (int, error)
}
