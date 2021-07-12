package api

import (
	"context"
	"time"

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
	StatisWin(ctx context.Context, id string) (*database.StatisWin, error)
	StatisWins(ctx context.Context, now time.Time, limit int) ([]database.StatisWin, error)
	WdpostEnablePartitionSeparate(ctx context.Context, enable bool) error
	WdpostSetPartitionNumber(ctx context.Context, number int) error
	WdPostGetLog(ctx context.Context, index uint64) ([]WdPoStLog, error)
}

type HlmMinerSector interface {
	RunPledgeSector(context.Context) error
	StatusPledgeSector(context.Context) (int, error)
	StopPledgeSector(context.Context) error
	RebuildPledgeSector(context.Context, string, uint64) error

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
	GetProvingCheckTimeout(ctx context.Context) (time.Duration, error)
	SetProvingCheckTimeout(ctx context.Context, timeout time.Duration) error
	GetFaultCheckTimeout(ctx context.Context) (time.Duration, error)
	SetFaultCheckTimeout(ctx context.Context, timeout time.Duration) error
}

type HlmMinerWorker interface {
	PauseSeal(ctx context.Context, pause int32) error

	WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error)
	WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error)

	WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error)
	WorkerGcLock(ctx context.Context, workerId string) ([]string, error)
	WorkerInfo(ctx context.Context, wid string) (*database.WorkerInfo, error)
	WorkerSearch(ctx context.Context, ip string) ([]database.WorkerInfo, error)
	WorkerDisable(ctx context.Context, wid string, disable bool) error
}
