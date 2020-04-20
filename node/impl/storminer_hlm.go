package impl

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/sector-storage/database"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/sector-storage/tarutil"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
)

func (sm *StorageMinerAPI) remoteGetSector(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id := vars["id"]
	if len(id) == 0 {
		log.Error("sector id not found")
		w.WriteHeader(500)
		return
	}

	path := sm.StorageMgr.Prover.(*ffiwrapper.Sealer).SectorPath(vars["type"], id)
	stat, err := os.Stat(string(path))
	if err != nil {
		log.Error(err)
		w.WriteHeader(500)
		return
	}

	var rd io.Reader
	if stat.IsDir() {
		rd, err = tarutil.TarDirectory(string(path))
		w.Header().Set("Content-Type", "application/x-tar")
	} else {
		rd, err = os.OpenFile(string(path), os.O_RDONLY, 0644)
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if err != nil {
		log.Error(err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
	if _, err := io.Copy(w, rd); err != nil {
		log.Error(err)
		return
	}
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

// Message communication
func (sm *StorageMinerAPI) MpoolPushMessage(ctx context.Context, msg *types.Message) (*types.SignedMessage, error) {
	return sm.Full.MpoolPushMessage(ctx, msg)
}
func (sm *StorageMinerAPI) StateWaitMsg(ctx context.Context, id cid.Cid) (*api.MsgLookup, error) {
	return sm.Full.StateWaitMsg(ctx, id)
}

func (sm *StorageMinerAPI) WalletSign(ctx context.Context, addr address.Address, data []byte) (*crypto.Signature, error) {
	return sm.Full.WalletSign(ctx, addr, data)
}
func (sm *StorageMinerAPI) SectorsListAll(context.Context) ([]api.SectorInfo, error) {
	sectors, err := sm.Miner.ListSectors()
	if err != nil {
		return nil, err
	}

	out := []api.SectorInfo{}
	for _, sector := range sectors {
		out = append(out, api.SectorInfo{
			State:    sector.State,
			SectorID: sector.SectorNumber,
			// TODO: more?
		})
	}
	return out, nil
}

func (sm *StorageMinerAPI) WorkerAddress(ctx context.Context, act address.Address, tsk types.TipSetKey) (address.Address, error) {
	mInfo, err := sm.Full.StateMinerInfo(ctx, act, tsk)
	if err != nil {
		return address.Address{}, err
	}
	return mInfo.Worker, nil
}

func (sm *StorageMinerAPI) WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).WorkerStats(), nil
}
func (sm *StorageMinerAPI) WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).WorkerRemoteStats()
}
func (sm *StorageMinerAPI) WorkerQueue(ctx context.Context, cfg ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AddWorker(ctx, cfg)
}
func (sm *StorageMinerAPI) WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error) {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskWorking(workerId)
}
func (sm *StorageMinerAPI) WorkerPushing(ctx context.Context, taskKey string) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskPushing(ctx, taskKey)
}

func (sm *StorageMinerAPI) WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).TaskDone(ctx, res)
}
func (sm *StorageMinerAPI) WorkerDisable(ctx context.Context, wid string, disable bool) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).DisableWorker(ctx, wid, disable)
}
func (sm *StorageMinerAPI) AddHLMStorage(ctx context.Context, sInfo database.StorageInfo) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).AddStorage(ctx, sInfo)
}
func (sm *StorageMinerAPI) DisableHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).DisableStorage(ctx, id)
}
func (sm *StorageMinerAPI) MountHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).MountStorage(ctx, id)
}

func (sm *StorageMinerAPI) UMountHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).UMountStorage(ctx, id)
}

func (sm *StorageMinerAPI) RelinkHLMStorage(ctx context.Context, id int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).RelinkStorage(ctx, id)
}
func (sm *StorageMinerAPI) ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error {
	return sm.StorageMgr.Prover.(*ffiwrapper.Sealer).ScaleStorage(ctx, id, size, work)
}
