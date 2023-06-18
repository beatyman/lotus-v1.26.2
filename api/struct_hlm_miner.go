// Doc for apistruct
//
// 1, desgian lotus/api/api_storage_hlm.go
// 2, implement the ./permissioned.go
// 3, import to lotus/api/client/client.go
// 4, import to lotus/metrics/proxy.go
package api

import (
	"context"
	"time"

	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type HlmMinerProxyStruct struct {
	Internal struct {
		ProxyAutoSelect func(ctx context.Context, on bool) error                                 `perm:"admin"`
		ProxyChange     func(ctx context.Context, idx int) error                                 `perm:"admin"`
		ProxyStatus     func(ctx context.Context, cond ProxyStatCondition) (*ProxyStatus, error) `perm:"read"`
		ProxyReload     func(ctx context.Context) error                                          `perm:"admin"`
	}
}

type HlmMinerProvingStruct struct {
	Internal struct {
		Testing                       func(ctx context.Context, fnName string, args []string) error                     `perm:"admin"`
		StatisWin                     func(ctx context.Context, id string) (*database.StatisWin, error)                 `perm:"read"`
		StatisWins                    func(ctx context.Context, now time.Time, limit int) ([]database.StatisWin, error) `perm:"read"`
		WdpostEnablePartitionSeparate func(ctx context.Context, enable bool) error                                      `perm:"admin"`
		WdpostSetPartitionNumber      func(ctx context.Context, number int) error                                       `perm:"admin"`
		WdPostGetLog                  func(ctx context.Context, index uint64) ([]WdPoStLog, error)                      `perm:"read"`
	}
}

type HlmMinerSectorStruct struct {
	Internal struct {
		RunPledgeSector     func(context.Context) error                 `perm:"admin"`
		StatusPledgeSector  func(context.Context) (int, error)          `perm:"read"`
		StopPledgeSector    func(context.Context) error                 `perm:"admin"`
		RebuildPledgeSector func(context.Context, string, uint64) error `perm:"admin"`

		HlmSectorGetState   func(ctx context.Context, sid string) (*database.SectorInfo, error)                     `perm:"read"`
		HlmSectorSetState   func(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error) `perm:"admin"`
		HlmSectorListAll    func(context.Context) ([]SectorInfo, error)                                             `perm:"read"`
		HlmSectorFile       func(ctx context.Context, sid string) (*storiface.SectorFile, error)                    `perm:"read"`
		HlmSectorCheck      func(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error)     `perm:"read"`
		HlmSectorGetStartID func(ctx context.Context) (uint64, error)                                               `perm:"read"`
		HlmSectorSetStartID func(ctx context.Context, baseID uint64) error                                          `perm:"admin"`
	}
}

type HlmMinerStorageStruct struct {
	Internal struct {
		// for miner node
		StatusMinerStorage func(ctx context.Context) ([]byte, error) `perm:"read"`

		// for storage nodes
		VerHLMStorage          func(ctx context.Context) (int64, error)                                                     `perm:"read"`
		GetHLMStorage          func(ctx context.Context, id int64) (*database.StorageInfo, error)                           `perm:"read"`
		SearchHLMStorage       func(ctx context.Context, ip string) ([]database.StorageInfo, error)                         `perm:"read"`
		AddHLMStorage          func(ctx context.Context, sInfo *database.StorageAuth) error                                 `perm:"admin"`
		DisableHLMStorage      func(ctx context.Context, id int64, disable bool) error                                      `perm:"admin"`
		MountHLMStorage        func(ctx context.Context, id int64) error                                                    `perm:"admin"`
		UMountHLMStorage       func(ctx context.Context, id int64) error                                                    `perm:"admin"`
		RelinkHLMStorage       func(ctx context.Context, id int64, minerAddr string) error                                  `perm:"admin"`
		ReplaceHLMStorage      func(ctx context.Context, info *database.StorageAuth) error                                  `perm:"write"`
		ScaleHLMStorage        func(ctx context.Context, id int64, size int64, work int64) error                            `perm:"admin"`
		StatusHLMStorage       func(ctx context.Context, id int64, timeout time.Duration) ([]database.StorageStatus, error) `perm:"read"`
		GetProvingCheckTimeout func(ctx context.Context) (time.Duration, error)                                             `perm:"read"`
		SetProvingCheckTimeout func(ctx context.Context, timeout time.Duration) error                                       `perm:"write"`
		GetFaultCheckTimeout   func(ctx context.Context) (time.Duration, error)                                             `perm:"read"`
		SetFaultCheckTimeout   func(ctx context.Context, timeout time.Duration) error                                       `perm:"write"`
	}
}

type HlmMinerWorkerStruct struct {
	Internal struct {
		PauseSeal       func(ctx context.Context, pause int32) error                                `perm:"write"`
		WorkerStatus    func(context.Context) (ffiwrapper.WorkerStats, error)                       `perm:"read"`
		WorkerStatusAll func(context.Context) ([]ffiwrapper.WorkerRemoteStats, error)               `perm:"read"`
		WorkerWorking   func(ctx context.Context, workerId string) (database.WorkingSectors, error) `perm:"read"`
		WorkerGcLock    func(ctx context.Context, workerId string) ([]string, error)                `perm:"write"`
		WorkerInfo      func(ctx context.Context, wid string) (*database.WorkerInfo, error)         `perm:"read"`
		WorkerSearch    func(ctx context.Context, ip string) ([]database.WorkerInfo, error)         `perm:"read"`
		WorkerDisable   func(ctx context.Context, wid string, disable bool) error                   `perm:"admin"`
	}
}

// implements by hlm start
func (c *HlmMinerProxyStruct) ProxyAutoSelect(ctx context.Context, on bool) error {
	return c.Internal.ProxyAutoSelect(ctx, on)
}
func (c *HlmMinerProxyStruct) ProxyChange(ctx context.Context, idx int) error {
	return c.Internal.ProxyChange(ctx, idx)
}
func (c *HlmMinerProxyStruct) ProxyStatus(ctx context.Context, cond ProxyStatCondition) (*ProxyStatus, error) {
	return c.Internal.ProxyStatus(ctx, cond)
}
func (c *HlmMinerProxyStruct) ProxyReload(ctx context.Context) error {
	return c.Internal.ProxyReload(ctx)
}

func (c *HlmMinerProvingStruct) Testing(ctx context.Context, fnName string, args []string) error {
	return c.Internal.Testing(ctx, fnName, args)
}
func (c *HlmMinerProvingStruct) StatisWin(ctx context.Context, id string) (*database.StatisWin, error) {
	return c.Internal.StatisWin(ctx, id)
}
func (c *HlmMinerProvingStruct) StatisWins(ctx context.Context, now time.Time, limit int) ([]database.StatisWin, error) {
	return c.Internal.StatisWins(ctx, now, limit)
}
func (c *HlmMinerProvingStruct) WdpostEnablePartitionSeparate(ctx context.Context, enable bool) error {
	return c.Internal.WdpostEnablePartitionSeparate(ctx, enable)
}
func (c *HlmMinerProvingStruct) WdpostSetPartitionNumber(ctx context.Context, number int) error {
	return c.Internal.WdpostSetPartitionNumber(ctx, number)
}
func (c *HlmMinerProvingStruct) WdPostGetLog(ctx context.Context, index uint64) ([]WdPoStLog, error) {
	return c.Internal.WdPostGetLog(ctx, index)
}

func (c *HlmMinerSectorStruct) RunPledgeSector(ctx context.Context) error {
	return c.Internal.RunPledgeSector(ctx)
}
func (c *HlmMinerSectorStruct) StatusPledgeSector(ctx context.Context) (int, error) {
	return c.Internal.StatusPledgeSector(ctx)
}
func (c *HlmMinerSectorStruct) StopPledgeSector(ctx context.Context) error {
	return c.Internal.StopPledgeSector(ctx)
}
func (c *HlmMinerSectorStruct) RebuildPledgeSector(ctx context.Context, sectorId string, storageId uint64) error {
	return c.Internal.RebuildPledgeSector(ctx, sectorId, storageId)
}
func (c *HlmMinerSectorStruct) HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	return c.Internal.HlmSectorGetState(ctx, sid)
}
func (c *HlmMinerSectorStruct) HlmSectorSetState(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error) {
	return c.Internal.HlmSectorSetState(ctx, sid, memo, state, force, reset)
}
func (c *HlmMinerSectorStruct) HlmSectorListAll(ctx context.Context) ([]SectorInfo, error) {
	return c.Internal.HlmSectorListAll(ctx)
}
func (c *HlmMinerSectorStruct) HlmSectorFile(ctx context.Context, sid string) (*storiface.SectorFile, error) {
	return c.Internal.HlmSectorFile(ctx, sid)
}
func (c *HlmMinerSectorStruct) HlmSectorCheck(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error) {
	return c.Internal.HlmSectorCheck(ctx, sid, timeout)
}
func (c *HlmMinerSectorStruct) HlmSectorGetStartID(ctx context.Context) (uint64, error) {
	return c.Internal.HlmSectorGetStartID(ctx)
}
func (c *HlmMinerSectorStruct) HlmSectorSetStartID(ctx context.Context, baseID uint64) error {
	return c.Internal.HlmSectorSetStartID(ctx, baseID)
}

// HlmMinerStorageStruct
func (c *HlmMinerStorageStruct) StatusMinerStorage(ctx context.Context) ([]byte, error) {
	return c.Internal.StatusMinerStorage(ctx)
}

func (c *HlmMinerStorageStruct) VerHLMStorage(ctx context.Context) (int64, error) {
	return c.Internal.VerHLMStorage(ctx)
}
func (c *HlmMinerStorageStruct) GetHLMStorage(ctx context.Context, id int64) (*database.StorageInfo, error) {
	return c.Internal.GetHLMStorage(ctx, id)
}
func (c *HlmMinerStorageStruct) SearchHLMStorage(ctx context.Context, ip string) ([]database.StorageInfo, error) {
	return c.Internal.SearchHLMStorage(ctx, ip)
}
func (c *HlmMinerStorageStruct) AddHLMStorage(ctx context.Context, sInfo *database.StorageAuth) error {
	return c.Internal.AddHLMStorage(ctx, sInfo)
}
func (c *HlmMinerStorageStruct) DisableHLMStorage(ctx context.Context, id int64, disable bool) error {
	return c.Internal.DisableHLMStorage(ctx, id, disable)
}
func (c *HlmMinerStorageStruct) MountHLMStorage(ctx context.Context, id int64) error {
	return c.Internal.MountHLMStorage(ctx, id)
}
func (c *HlmMinerStorageStruct) UMountHLMStorage(ctx context.Context, id int64) error {
	return c.Internal.UMountHLMStorage(ctx, id)
}
func (c *HlmMinerStorageStruct) RelinkHLMStorage(ctx context.Context, id int64, minerAddr string) error {
	return c.Internal.RelinkHLMStorage(ctx, id, minerAddr)
}
func (c *HlmMinerStorageStruct) ReplaceHLMStorage(ctx context.Context, info *database.StorageAuth) error {
	return c.Internal.ReplaceHLMStorage(ctx, info)
}
func (c *HlmMinerStorageStruct) ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error {
	return c.Internal.ScaleHLMStorage(ctx, id, size, work)
}
func (c *HlmMinerStorageStruct) StatusHLMStorage(ctx context.Context, id int64, timeout time.Duration) ([]database.StorageStatus, error) {
	return c.Internal.StatusHLMStorage(ctx, id, timeout)
}

func (c *HlmMinerStorageStruct) GetProvingCheckTimeout(ctx context.Context) (time.Duration, error) {
	return c.Internal.GetProvingCheckTimeout(ctx)
}
func (c *HlmMinerStorageStruct) SetProvingCheckTimeout(ctx context.Context, timeout time.Duration) error {
	return c.Internal.SetProvingCheckTimeout(ctx, timeout)
}
func (c *HlmMinerStorageStruct) GetFaultCheckTimeout(ctx context.Context) (time.Duration, error) {
	return c.Internal.GetFaultCheckTimeout(ctx)
}
func (c *HlmMinerStorageStruct) SetFaultCheckTimeout(ctx context.Context, timeout time.Duration) error {
	return c.Internal.SetFaultCheckTimeout(ctx, timeout)
}

func (c *HlmMinerWorkerStruct) PauseSeal(ctx context.Context, pause int32) error {
	return c.Internal.PauseSeal(ctx, pause)
}
func (c *HlmMinerWorkerStruct) WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error) {
	return c.Internal.WorkerStatus(ctx)
}
func (c *HlmMinerWorkerStruct) WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error) {
	return c.Internal.WorkerStatusAll(ctx)
}
func (c *HlmMinerWorkerStruct) WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error) {
	return c.Internal.WorkerWorking(ctx, workerId)
}
func (c *HlmMinerWorkerStruct) WorkerGcLock(ctx context.Context, workerId string) ([]string, error) {
	return c.Internal.WorkerGcLock(ctx, workerId)
}
func (c *HlmMinerWorkerStruct) WorkerInfo(ctx context.Context, wid string) (*database.WorkerInfo, error) {
	return c.Internal.WorkerInfo(ctx, wid)
}
func (c *HlmMinerWorkerStruct) WorkerSearch(ctx context.Context, ip string) ([]database.WorkerInfo, error) {
	return c.Internal.WorkerSearch(ctx, ip)
}
func (c *HlmMinerWorkerStruct) WorkerDisable(ctx context.Context, wid string, disable bool) error {
	return c.Internal.WorkerDisable(ctx, wid, disable)
}

type HlmMinerMarketStruct struct {
	Internal struct {
		NewMarketDealFSTMP func(context.Context) (string, error)                                               `perm:"admin"`
		AddMarketDeal      func(context.Context, *database.MarketDealInfo) error                               `perm:"admin"`
		GetMarketDeal      func(context.Context, string) (*database.MarketDealInfo, error)                     `perm:"read"`
		GetMarketDealBySid func(context.Context, string) ([]database.MarketDealInfo, error)                    `perm:"read"`
		ListMarketDeal     func(context.Context, time.Time, time.Time, int) ([]database.MarketDealInfo, error) `perm:"read"`
		UpdateMarketDeal   func(context.Context, *database.MarketDealInfo) error                               `perm:"admin"`
	}
}
// FStarMinerMarketStruct
func (c *HlmMinerMarketStruct) NewMarketDealFSTMP(ctx context.Context) (string, error) {
	return c.Internal.NewMarketDealFSTMP(ctx)
}
func (c *HlmMinerMarketStruct) AddMarketDeal(ctx context.Context, deal *database.MarketDealInfo) error {
	return c.Internal.AddMarketDeal(ctx, deal)
}
func (c *HlmMinerMarketStruct) GetMarketDeal(ctx context.Context, propCid string) (*database.MarketDealInfo, error) {
	return c.Internal.GetMarketDeal(ctx, propCid)
}
func (c *HlmMinerMarketStruct) GetMarketDealBySid(ctx context.Context, sid string) ([]database.MarketDealInfo, error) {
	return c.Internal.GetMarketDealBySid(ctx, sid)
}
func (c *HlmMinerMarketStruct) ListMarketDeal(ctx context.Context, beginTime, endTime time.Time, state int) ([]database.MarketDealInfo, error) {
	return c.Internal.ListMarketDeal(ctx, beginTime, endTime, state)
}
func (c *HlmMinerMarketStruct) UpdateMarketDeal(ctx context.Context, deal *database.MarketDealInfo) error {
	return c.Internal.UpdateMarketDeal(ctx, deal)
}
var _ HlmMinerProxy = &HlmMinerProxyStruct{}
var _ HlmMinerProving = &HlmMinerProvingStruct{}
var _ HlmMinerSector = &HlmMinerSectorStruct{}
var _ HlmMinerStorage = &HlmMinerStorageStruct{}
var _ HlmMinerWorker = &HlmMinerWorkerStruct{}
var _ HlmMinerMarket= &HlmMinerMarketStruct{}