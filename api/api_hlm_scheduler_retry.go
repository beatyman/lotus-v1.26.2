package api

import (
	"context"
	"strings"
	"time"

	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

type RetryHlmMinerSchedulerAPI struct {
	HlmMinerSchedulerAPI
}

func (a *RetryHlmMinerSchedulerAPI) RetryEnable(err error) bool {
	return err != nil &&
		(strings.Contains(err.Error(), "websocket connection closed") ||
			strings.Contains(err.Error(), "connection refused"))
}

func (a *RetryHlmMinerSchedulerAPI) RetryAuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error) {
	var (
		err error
		out []byte
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.AuthNew(ctx, perms); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryActorSectorSize(ctx context.Context, addr address.Address) (abi.SectorSize, error) {
	var (
		err error
		out abi.SectorSize
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.ActorSectorSize(ctx, addr); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerDone(ctx context.Context, res ffiwrapper.SealRes) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.WorkerDone(ctx, res); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerLock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.WorkerLock(ctx, workerId, taskKey, memo, sectorState); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerUnlock(ctx context.Context, workerId, taskKey, memo string, sectorState int) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.WorkerUnlock(ctx, workerId, taskKey, memo, sectorState); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetrySelectCommit2Service(ctx context.Context, sid abi.SectorID) (*ffiwrapper.Commit2Worker, error) {
	var (
		err error
		out *ffiwrapper.Commit2Worker
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.SelectCommit2Service(ctx, sid); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryUnlockGPUService(ctx context.Context, rst *ffiwrapper.Commit2Result) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.UnlockGPUService(ctx, rst); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerWorkingById(ctx context.Context, sid []string) (database.WorkingSectors, error) {
	var (
		err error
		out database.WorkingSectors
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.WorkerWorkingById(ctx, sid); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerAddConn(ctx context.Context, wid string, num int) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.WorkerAddConn(ctx, wid, num); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerPreConnV1(ctx context.Context, skipWid []string) (*database.WorkerInfo, error) {
	var (
		err error
		out *database.WorkerInfo
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.WorkerPreConnV1(ctx, skipWid); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryWorkerMinerConn(ctx context.Context) (int, error) {
	var (
		err error
		out int
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.WorkerMinerConn(ctx); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryCommitStorageNode(ctx context.Context, sectorId string, kind int) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.CommitStorageNode(ctx, sectorId, kind); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryPreStorageNode(ctx context.Context, sectorId, clientIp string, kind int) (*database.StorageInfo, error) {
	var (
		err error
		out *database.StorageInfo
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.PreStorageNode(ctx, sectorId, clientIp, kind); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryDelHLMStorageTmpAuth(ctx context.Context, id int64, sid string) error {
	var err error
	for i := 0; true; i++ {
		if err = a.HlmMinerSchedulerAPI.DelHLMStorageTmpAuth(ctx, id, sid); err == nil {
			return nil
		}
		if !a.RetryEnable(err) {
			return err
		}
		time.Sleep(time.Second * 10)
	}
	return err
}

func (a *RetryHlmMinerSchedulerAPI) RetryNewHLMStorageTmpAuth(ctx context.Context, id int64, sid string) (string, error) {
	var (
		err error
		out string
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.NewHLMStorageTmpAuth(ctx, id, sid); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryChecksumStorage(ctx context.Context, ver int64) ([]database.StorageInfo, error) {
	var (
		err error
		out []database.StorageInfo
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.ChecksumStorage(ctx, ver); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}

func (a *RetryHlmMinerSchedulerAPI) RetryHlmSectorGetState(ctx context.Context, sid string) (*database.SectorInfo, error) {
	var (
		err error
		out *database.SectorInfo
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.HlmSectorGetState(ctx, sid); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}
func (a *RetryHlmMinerSchedulerAPI) RetryGetStorage(ctx context.Context, storageId int64) (*database.StorageInfo, error) {
	var (
		err error
		out *database.StorageInfo
	)
	for i := 0; true; i++ {
		if out, err = a.HlmMinerSchedulerAPI.GetStorage(ctx, storageId); err == nil {
			return out, nil
		}
		if !a.RetryEnable(err) {
			return out, err
		}
		time.Sleep(time.Second * 10)
	}
	return out, err
}