package impl

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/go-sectorbuilder/database"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/tarutil"
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

	path := sm.SectorBuilder.SectorPath(vars["type"], id)
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
			SectorID: sector.SectorID,
			// TODO: more?
		})
	}
	return out, nil
}

func (sm *StorageMinerAPI) WorkerAddress(ctx context.Context, act address.Address, tsk types.TipSetKey) (address.Address, error) {
	return sm.Full.StateMinerWorker(ctx, act, tsk)
}

func (sm *StorageMinerAPI) WorkerStatsAll(ctx context.Context) ([]sectorbuilder.WorkerRemoteStats, error) {
	return sm.SectorBuilder.WorkerRemoteStats()
}
func (sm *StorageMinerAPI) WorkerQueue(ctx context.Context, cfg sectorbuilder.WorkerCfg) (<-chan sectorbuilder.WorkerTask, error) {
	return sm.SectorBuilder.AddWorker(ctx, cfg)
}
func (sm *StorageMinerAPI) WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error) {
	return sm.SectorBuilder.TaskWorking(workerId)
}
func (sm *StorageMinerAPI) WorkerPushing(ctx context.Context, taskKey string) error {
	return sm.SectorBuilder.TaskPushing(ctx, taskKey)
}

func (sm *StorageMinerAPI) WorkerDone(ctx context.Context, res sectorbuilder.SealRes) error {
	return sm.SectorBuilder.TaskDone(ctx, res)
}
func (sm *StorageMinerAPI) WorkerDisable(ctx context.Context, wid string, disable bool) error {
	return sm.SectorBuilder.DisableWorker(ctx, wid, disable)
}
func (sm *StorageMinerAPI) AddHLMStorage(ctx context.Context, sInfo database.StorageInfo) error {
	return sm.SectorBuilder.AddStorage(ctx, sInfo)
}
func (sm *StorageMinerAPI) DisableHLMStorage(ctx context.Context, id int64) error {
	return sm.SectorBuilder.DisableStorage(ctx, id)
}
func (sm *StorageMinerAPI) MountHLMStorage(ctx context.Context, id int64) error {
	return sm.SectorBuilder.MountStorage(ctx, id)
}

func (sm *StorageMinerAPI) UMountHLMStorage(ctx context.Context, id int64) error {
	return sm.SectorBuilder.UMountStorage(ctx, id)
}

func (sm *StorageMinerAPI) RelinkHLMStorage(ctx context.Context, id int64) error {
	return sm.SectorBuilder.RelinkStorage(ctx, id)
}
func (sm *StorageMinerAPI) ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error {
	return sm.SectorBuilder.ScaleStorage(ctx, id, size, work)
}
