// Doc for apistruct
//
// 1, desgian lotus/api/api_storage_hlm.go
// 2, implement the ./permissioned.go
// 3, import to lotus/api/client/client.go
// 4, import to lotus/metrics/proxy.go
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

type HlmMinerSchedulerStruct struct {
	Internal struct {
		Version func(context.Context) (APIVersion, error) `perm:"read"`

		AuthVerify func(ctx context.Context, token string) ([]auth.Permission, error) `perm:"read"`
		AuthNew    func(ctx context.Context, perms []auth.Permission) ([]byte, error) `perm:"admin"`

		ActorAddress    func(context.Context) (address.Address, error)                                   `perm:"read"`
		WorkerAddress   func(context.Context, address.Address, types.TipSetKey) (address.Address, error) `perm:"read"`
		ActorSectorSize func(context.Context, address.Address) (abi.SectorSize, error)                   `perm:"read"`

		SelectCommit2Service func(context.Context, abi.SectorID) (*ffiwrapper.Commit2Worker, error) `perm:"write"`
		UnlockGPUService     func(ctx context.Context, rst *ffiwrapper.Commit2Result) error         `perm:"write"`

		WorkerQueue func(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) `perm:"write"`
		WorkerDone  func(ctx context.Context, res ffiwrapper.SealRes) error                                   `perm:"write"`

		WorkerFileWatch func(ctx context.Context, res ffiwrapper.WorkerCfg) error `perm:"write"`

		WorkerWorkingById func(ctx context.Context, sid []string) (database.WorkingSectors, error) `perm:"read"`

		WorkerAddConn   func(ctx context.Context, wid string, num int) error                      `perm:"write"`
		WorkerPreConn   func(ctx context.Context) (*database.WorkerInfo, error)                   `perm:"read"`
		WorkerPreConnV1 func(ctx context.Context, skipWid []string) (*database.WorkerInfo, error) `perm:"write"`
		WorkerMinerConn func(ctx context.Context) (int, error)                                    `perm:"write"`

		WorkerLock   func(ctx context.Context, workerId, taskKey, memo string, sectorState int) error `perm:"write"`
		WorkerUnlock func(ctx context.Context, workerId, taskKey, memo string, sectorState int) error `perm:"write"`

		ChecksumStorage         func(ctx context.Context, ver int64) ([]database.StorageInfo, error)                          `perm:"read"`
		NewHLMStorageTmpAuth    func(ctx context.Context, id int64, sid string) (string, error)                               `perm:"admin"`
		DelHLMStorageTmpAuth    func(ctx context.Context, id int64, sid string) error                                         `perm:"admin"`
		PreStorageNode          func(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) `perm:"write"`
		CommitStorageNode       func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		CancelStorageNode       func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		AcquireStorageConnCount func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		ReleaseStorageConnCount func(ctx context.Context, sectorId string, kind int) error                                    `perm:"write"`
		HlmSectorGetState       func(ctx context.Context, sid string) (*database.SectorInfo, error)                           `perm:"read"`

		//check worker busyTask
		GetWorkerBusyTask func(ctx context.Context, wid string) (int, error) `perm:"read"`
		//disable worker
		RequestDisableWorker func(ctx context.Context, wid string) error `perm:"read"`
		//miner info
		GetMinerInfo           func(ctx context.Context) string                                           `perm:"read"`
		PutStatisSeal          func(ctx context.Context, st database.StatisSeal) error                    `perm:"write"`
		GetStorage             func(ctx context.Context, storageId int64) (*database.StorageInfo, error)  `perm:"read"`
		GetMarketDealInfo      func(ctx context.Context, propID string) (*database.MarketDealInfo, error) `perm:"read"`
		GetMarketDealInfoBySid func(ctx context.Context, sid string) ([]database.MarketDealInfo, error)   `perm:"read"`
	}
}

func (c *HlmMinerSchedulerStruct) Version(ctx context.Context) (APIVersion, error) {
	return c.Internal.Version(ctx)
}
func (c *HlmMinerSchedulerStruct) AuthVerify(ctx context.Context, token string) ([]auth.Permission, error) {
	return c.Internal.AuthVerify(ctx, token)
}
func (c *HlmMinerSchedulerStruct) AuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error) {
	return c.Internal.AuthNew(ctx, perms)
}
func (c *HlmMinerSchedulerStruct) ActorAddress(ctx context.Context) (address.Address, error) {
	return c.Internal.ActorAddress(ctx)
}
func (c *HlmMinerSchedulerStruct) ActorSectorSize(ctx context.Context, addr address.Address) (abi.SectorSize, error) {
	return c.Internal.ActorSectorSize(ctx, addr)
}
func (c *HlmMinerSchedulerStruct) WorkerAddress(ctx context.Context, act address.Address, tsk types.TipSetKey) (address.Address, error) {
	return c.Internal.WorkerAddress(ctx, act, tsk)
}

func (c *HlmMinerSchedulerStruct) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*ffiwrapper.Commit2Worker, error) {
	return c.Internal.SelectCommit2Service(ctx, sector)
}
func (c *HlmMinerSchedulerStruct) UnlockGPUService(ctx context.Context, rst *ffiwrapper.Commit2Result) error {
	return c.Internal.UnlockGPUService(ctx, rst)
}

func (c *HlmMinerSchedulerStruct) WorkerQueue(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) {
	return c.Internal.WorkerQueue(ctx, cfg)
}
func (c *HlmMinerSchedulerStruct) WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error {
	return c.Internal.WorkerDone(ctx, res)
}

func (c *HlmMinerSchedulerStruct) WorkerFileWatch(ctx context.Context, res ffiwrapper.WorkerCfg) error {
	return c.Internal.WorkerFileWatch(ctx, res)
}

func (c *HlmMinerSchedulerStruct) WorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error) {
	return c.Internal.WorkerWorkingById(ctx, sid)
}

func (c *HlmMinerSchedulerStruct) WorkerAddConn(ctx context.Context, wid string, num int) error {
	return c.Internal.WorkerAddConn(ctx, wid, num)
}
func (c *HlmMinerSchedulerStruct) WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error) {
	return c.Internal.WorkerPreConn(ctx)
}
func (c *HlmMinerSchedulerStruct) WorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error) {
	return c.Internal.WorkerPreConnV1(ctx, skipWid)
}
func (c *HlmMinerSchedulerStruct) WorkerMinerConn(ctx context.Context) (int, error) {
	return c.Internal.WorkerMinerConn(ctx)
}

func (c *HlmMinerSchedulerStruct) WorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return c.Internal.WorkerLock(ctx, workerId, taskKey, memo, sectorState)
}
func (c *HlmMinerSchedulerStruct) WorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	return c.Internal.WorkerUnlock(ctx, workerId, taskKey, memo, sectorState)
}

func (c *HlmMinerSchedulerStruct) ChecksumStorage(ctx context.Context, sumVer int64) ([]database.StorageInfo, error) {
	return c.Internal.ChecksumStorage(ctx, sumVer)
}
func (c *HlmMinerSchedulerStruct) NewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error) {
	return c.Internal.NewHLMStorageTmpAuth(ctx, id, sid)
}
func (c *HlmMinerSchedulerStruct) DelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error {
	return c.Internal.DelHLMStorageTmpAuth(ctx, id, sid)
}
func (c *HlmMinerSchedulerStruct) PreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) {
	return c.Internal.PreStorageNode(ctx, sectorId, clientIp, kind)
}
func (c *HlmMinerSchedulerStruct) CommitStorageNode(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.CommitStorageNode(ctx, sectorId, kind)
}

func (c *HlmMinerSchedulerStruct) CancelStorageNode(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.CancelStorageNode(ctx, sectorId, kind)
}
func (c *HlmMinerSchedulerStruct) AcquireStorageConnCount(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.AcquireStorageConnCount(ctx, sectorId, kind)
}
func (c *HlmMinerSchedulerStruct) ReleaseStorageConnCount(ctx context.Context, sectorId string, kind int) error {
	return c.Internal.ReleaseStorageConnCount(ctx, sectorId, kind)
}
func (c *HlmMinerSchedulerStruct) HlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	return c.Internal.HlmSectorGetState(ctx, sid)
}
func (c *HlmMinerSchedulerStruct) GetWorkerBusyTask(ctx context.Context, wid string) (int, error) {
	return c.Internal.GetWorkerBusyTask(ctx, wid)
}
func (c *HlmMinerSchedulerStruct) RequestDisableWorker(ctx context.Context, wid string) error {
	return c.Internal.RequestDisableWorker(ctx, wid)
}

func (c *HlmMinerSchedulerStruct) GetMinerInfo(ctx context.Context) string {
	return c.Internal.GetMinerInfo(ctx)
}
func (c *HlmMinerSchedulerStruct) PutStatisSeal(ctx context.Context, st database.StatisSeal) error {
	return c.Internal.PutStatisSeal(ctx, st)
}

func (c *HlmMinerSchedulerStruct) GetStorage(ctx context.Context, storageId int64) (*database.StorageInfo, error) {
	return c.Internal.GetStorage(ctx, storageId)
}
func (c *HlmMinerSchedulerStruct) GetMarketDealInfo(ctx context.Context, propID string) (*database.MarketDealInfo, error) {
	return c.Internal.GetMarketDealInfo(ctx, propID)
}
func (c *HlmMinerSchedulerStruct) GetMarketDealInfoBySid(ctx context.Context, sid string) ([]database.MarketDealInfo, error) {
	return c.Internal.GetMarketDealInfoBySid(ctx, sid)
}

var _ HlmMinerSchedulerAPI = &HlmMinerSchedulerStruct{}
