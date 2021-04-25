package apistruct

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
)

type HlmMinerProxyStruct struct {
	Internal struct {
		ProxyAutoSelect func(ctx context.Context, on bool) error                                         `perm:"admin"`
		ProxyChange     func(ctx context.Context, idx int) error                                         `perm:"admin"`
		ProxyStatus     func(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) `perm:"read"`
		ProxyReload     func(ctx context.Context) error                                                  `perm:"admin"`
	}
}

type HlmMinerProvingStruct struct {
	Internal struct {
		Testing                       func(ctx context.Context, fnName string, args []string) error `perm:"admin"`
		WdpostEnablePartitionSeparate func(ctx context.Context, enable bool) error                  `perm:"admin"`
		WdpostSetPartitionNumber      func(ctx context.Context, number int) error                   `perm:"admin"`
	}
}

type HlmMinerSectorStruct struct {
	Internal struct {
		RunPledgeSector    func(context.Context) error                                                             `perm:"admin"`
		StatusPledgeSector func(context.Context) (int, error)                                                      `perm:"read"`
		StopPledgeSector   func(context.Context) error                                                             `perm:"admin"`
		HlmSectorGetState  func(ctx context.Context, sid string) (*database.SectorInfo, error)                     `perm:"read"`
		HlmSectorSetState  func(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error) `perm:"admin"`
		HlmSectorListAll   func(context.Context) ([]api.SectorInfo, error)                                         `perm:"read"`
		HlmSectorFile      func(ctx context.Context, sid string) (*storage.SectorFile, error)                      `perm:"read"`
		HlmSectorCheck     func(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error)     `perm:"read"`
	}
}

type HlmMinerStorageStruct struct {
	Internal struct {
		// for miner node
		StatusMinerStorage func(ctx context.Context) ([]byte, error) `perm:"read"`

		// for storage nodes
		VerHLMStorage          func(ctx context.Context) (int64, error)                                                      `perm:"read"`
		GetHLMStorage          func(ctx context.Context, id int64) (*database.StorageInfo, error)                            `perm:"read"`
		SearchHLMStorage       func(ctx context.Context, ip string) ([]database.StorageInfo, error)                          `perm:"read"`
		AddHLMStorage          func(ctx context.Context, sInfo *database.StorageAuth) error                                  `perm:"admin"`
		DisableHLMStorage      func(ctx context.Context, id int64, disable bool) error                                       `perm:"admin"`
		MountHLMStorage        func(ctx context.Context, id int64) error                                                     `perm:"admin"`
		UMountHLMStorage       func(ctx context.Context, id int64) error                                                     `perm:"admin"`
		RelinkHLMStorage       func(ctx context.Context, id int64) error                                                     `perm:"admin"`
		ReplaceHLMStorage      func(ctx context.Context, info *database.StorageAuth) error                                   `perm:"write"`
		ScaleHLMStorage        func(ctx context.Context, id int64, size int64, work int64) error                             `perm:"admin"`
		StatusHLMStorage       func(ctx context.Context, id int64, timeout time.Duration) ([]database.StorageStatus, error)  `perm:"read"`
		NewHLMStorageTmpAuth   func(ctx context.Context, id int64, sid string) (string, error)                               `perm:"admin"`
		DelHLMStorageTmpAuth   func(ctx context.Context, id int64, sid string) error                                         `perm:"admin"`
		PreStorageNode         func(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) `perm:"write"`
		CommitStorageNode      func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		CancelStorageNode      func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		ChecksumStorage        func(ctx context.Context, ver int64) ([]database.StorageInfo, error)                          `perm:"read"`
		GetProvingCheckTimeout func(ctx context.Context) (time.Duration, error)                                              `perm:"read"`
		SetProvingCheckTimeout func(ctx context.Context, timeout time.Duration) error                                        `perm:"write"`
		GetFaultCheckTimeout   func(ctx context.Context) (time.Duration, error)                                              `perm:"read"`
		SetFaultCheckTimeout   func(ctx context.Context, timeout time.Duration) error                                        `perm:"write"`
	}
}

type HlmMinerWorkerStruct struct {
	Internal struct {
		SelectCommit2Service func(context.Context, abi.SectorID) (*ffiwrapper.WorkerCfg, error)                        `perm:"write"`
		UnlockGPUService     func(ctx context.Context, workerId, taskKey string) error                                 `perm:"write"`
		PauseSeal            func(ctx context.Context, pause int32) error                                              `perm:"write"`
		WorkerAddress        func(context.Context, address.Address, types.TipSetKey) (address.Address, error)          `perm:"read"`
		WorkerStatus         func(context.Context) (ffiwrapper.WorkerStats, error)                                     `perm:"read"`
		WorkerStatusAll      func(context.Context) ([]ffiwrapper.WorkerRemoteStats, error)                             `perm:"read"`
		WorkerQueue          func(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) `perm:"write"`
		WorkerWorking        func(ctx context.Context, workerId string) (database.WorkingSectors, error)               `perm:"read"`
		WorkerWorkingById    func(ctx context.Context, sid []string) (database.WorkingSectors, error)                  `perm:"read"`
		WorkerLock           func(ctx context.Context, workerId, taskKey, memo string, sectorState int) error          `perm:"write"`
		WorkerUnlock         func(ctx context.Context, workerId, taskKey, memo string, sectorState int) error          `perm:"write"`
		WorkerGcLock         func(ctx context.Context, workerId string) ([]string, error)                              `perm:"write"`
		WorkerDone           func(ctx context.Context, res ffiwrapper.SealRes) error                                   `perm:"write"`
		WorkerInfo           func(ctx context.Context, wid string) (*database.WorkerInfo, error)                       `perm:"read"`
		WorkerSearch         func(ctx context.Context, ip string) ([]database.WorkerInfo, error)                       `perm:"read"`
		WorkerDisable        func(ctx context.Context, wid string, disable bool) error                                 `perm:"admin"`
		WorkerAddConn        func(ctx context.Context, wid string, num int) error                                      `perm:"write"`
		WorkerPreConn        func(ctx context.Context) (*database.WorkerInfo, error)                                   `perm:"read"`
		WorkerPreConnV1      func(ctx context.Context, skipWid []string) (*database.WorkerInfo, error)                 `perm:"write"`
		WorkerMinerConn      func(ctx context.Context) (int, error)                                                    `perm:"write"`
	}
}

// implements by hlm start
func (c *HlmMinerProxyStruct) ProxyAutoSelect(ctx context.Context, on bool) error {
	return c.Internal.ProxyAutoSelect(ctx, on)
}
func (c *HlmMinerProxyStruct) ProxyChange(ctx context.Context, idx int) error {
	return c.Internal.ProxyChange(ctx, idx)
}
func (c *HlmMinerProxyStruct) ProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	return c.Internal.ProxyStatus(ctx, cond)
}
func (c *HlmMinerProxyStruct) ProxyReload(ctx context.Context) error {
	return c.Internal.ProxyReload(ctx)
}

func (c *HlmMinerProvingStruct) Testing(ctx context.Context, fnName string, args []string) error {
	return c.Internal.Testing(ctx, fnName, args)
}
func (c *HlmMinerProvingStruct) WdpostEnablePartitionSeparate(ctx context.Context, enable bool) error {
	return c.Internal.WdpostEnablePartitionSeparate(ctx, enable)
}
func (c *HlmMinerProvingStruct) WdpostSetPartitionNumber(ctx context.Context, number int) error {
	return c.Internal.WdpostSetPartitionNumber(ctx, number)
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
func (c *HlmMinerSectorStruct) HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	return c.Internal.HlmSectorGetState(ctx, sid)
}
func (c *HlmMinerSectorStruct) HlmSectorSetState(ctx context.Context, sid, memo string, state int, force, reset bool) (bool, error) {
	return c.Internal.HlmSectorSetState(ctx, sid, memo, state, force, reset)
}
func (c *HlmMinerSectorStruct) HlmSectorListAll(ctx context.Context) ([]api.SectorInfo, error) {
	return c.Internal.HlmSectorListAll(ctx)
}
func (c *HlmMinerSectorStruct) HlmSectorFile(ctx context.Context, sid string) (*storage.SectorFile, error) {
	return c.Internal.HlmSectorFile(ctx, sid)
}
func (c *HlmMinerSectorStruct) HlmSectorCheck(ctx context.Context, sid string, timeout time.Duration) (time.Duration, error) {
	return c.Internal.HlmSectorCheck(ctx, sid, timeout)
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
func (c *HlmMinerStorageStruct) RelinkHLMStorage(ctx context.Context, id int64) error {
	return c.Internal.RelinkHLMStorage(ctx, id)
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
func (c *HlmMinerStorageStruct) NewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error) {
	return c.Internal.NewHLMStorageTmpAuth(ctx, id, sid)
}
func (c *HlmMinerStorageStruct) DelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error {
	return c.Internal.DelHLMStorageTmpAuth(ctx, id, sid)
}
func (c *HlmMinerStorageStruct) PreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) {
	return c.Internal.PreStorageNode(ctx, sectorId, clientIp, kind)
}

func (c *HlmMinerStorageStruct) CommitStorageNode(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.CommitStorageNode(ctx, sectorId, kind)
}

func (c *HlmMinerStorageStruct) CancelStorageNode(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.CancelStorageNode(ctx, sectorId, kind)
}

func (c *HlmMinerStorageStruct) ChecksumStorage(ctx context.Context, sumVer int64) ([]database.StorageInfo, error) {
	return c.Internal.ChecksumStorage(ctx, sumVer)
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

func (c *HlmMinerWorkerStruct) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*ffiwrapper.WorkerCfg, error) {
	return c.Internal.SelectCommit2Service(ctx, sector)
}
func (c *HlmMinerWorkerStruct) UnlockGPUService(ctx context.Context, workerId, taskKey string) error {
	return c.Internal.UnlockGPUService(ctx, workerId, taskKey)
}
func (c *HlmMinerWorkerStruct) PauseSeal(ctx context.Context, pause int32) error {
	return c.Internal.PauseSeal(ctx, pause)
}
func (c *HlmMinerWorkerStruct) WorkerAddress(ctx context.Context, act address.Address, tsk types.TipSetKey) (address.Address, error) {
	return c.Internal.WorkerAddress(ctx, act, tsk)
}
func (c *HlmMinerWorkerStruct) WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error) {
	return c.Internal.WorkerStatus(ctx)
}
func (c *HlmMinerWorkerStruct) WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error) {
	return c.Internal.WorkerStatusAll(ctx)
}
func (c *HlmMinerWorkerStruct) WorkerQueue(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) {
	return c.Internal.WorkerQueue(ctx, cfg)
}
func (c *HlmMinerWorkerStruct) WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error) {
	return c.Internal.WorkerWorking(ctx, workerId)
}
func (c *HlmMinerWorkerStruct) WorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error) {
	return c.Internal.WorkerWorkingById(ctx, sid)
}
func (c *HlmMinerWorkerStruct) WorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return c.Internal.WorkerLock(ctx, workerId, taskKey, memo, sectorState)
}
func (c *HlmMinerWorkerStruct) WorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return c.Internal.WorkerUnlock(ctx, workerId, taskKey, memo, sectorState)
}
func (c *HlmMinerWorkerStruct) WorkerGcLock(ctx context.Context, workerId string) ([]string, error) {
	return c.Internal.WorkerGcLock(ctx, workerId)
}
func (c *HlmMinerWorkerStruct) WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error {
	return c.Internal.WorkerDone(ctx, res)
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
func (c *HlmMinerWorkerStruct) WorkerAddConn(ctx context.Context, wid string, num int) error {
	return c.Internal.WorkerAddConn(ctx, wid, num)
}
func (c *HlmMinerWorkerStruct) WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error) {
	return c.Internal.WorkerPreConn(ctx)
}
func (c *HlmMinerWorkerStruct) WorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error) {
	return c.Internal.WorkerPreConnV1(ctx, skipWid)
}
func (c *HlmMinerWorkerStruct) WorkerMinerConn(ctx context.Context) (int, error) {
	return c.Internal.WorkerMinerConn(ctx)
}
