package worker

import (
	"encoding/json"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/gwaylib/log"
	"strings"
)

// CollectSectorState ::q
func CollectSectorState(sectorStateInfo *buriedmodel.SectorState) error {
	sectorDataBytes, err := json.Marshal(sectorStateInfo)
	if err != nil {
		return err
	}

	reqData := &buriedmodel.BuriedDataCollectParams{
		DataType: "sector_state",
		Data:     sectorDataBytes,
	}
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		log.Println(err)
	}

	report.SendReport(reqDataBytes)
	if err != nil {
		return err
	}
	return nil
}

// CollectSectorState ::q
func CollectSectorStateInfo(task ffiwrapper.WorkerTask) error {
	// WorkerAddPiece       WorkerTaskType = 0
	// WorkerAddPieceDone                  = 1
	// WorkerPreCommit1                    = 10
	// WorkerPreCommit1Done                = 11
	// WorkerPreCommit2                    = 20
	// WorkerPreCommit2Done                = 21
	// WorkerCommit1                       = 30
	// WorkerCommit1Done                   = 31
	// WorkerCommit2                       = 40
	// WorkerCommit2Done                   = 41
	// WorkerFinalize                      = 50

	// workerCfg.ID, minerEndpoint, workerCfg.IP
	sectorStateInfo := &buriedmodel.SectorState{
		MinerID:    task.SectorStorage.SectorInfo.MinerId,
		WorkerID:   task.WorkerID,
		ClientIP:   task.SectorStorage.WorkerInfo.Ip,
		//SectorSize: task.SectorStorage.StorageInfo.SectorSize,
		// SectorID: storage.SectorName(m.minerSectorID(state.SectorNumber)),
		SectorID: task.SectorStorage.SectorInfo.ID,
	}
	taskKey := task.Key()
	// s-t01003-0_30
	keyParts := strings.Split(taskKey, "_")
	customState := keyParts[len(keyParts)-1 : len(keyParts)][0]
	switch customState {
	case "0":
		sectorStateInfo.State = "AddPieceDone"
	case "10":
		sectorStateInfo.State = "PreCommit1Done"
	case "20":
		sectorStateInfo.State = "PreCommit2Done"
	case "30":
		sectorStateInfo.State = "Commit1Done"
	case "40":
		sectorStateInfo.State = "Commit2Done"
	case "50":
		sectorStateInfo.State = "FinalizeSectorDone"
	default:
		sectorStateInfo.State = "UnknownState"
		sectorStateInfo.Msg = "Unknown task state"
	}

	sectorsDataBytes, err := json.Marshal(sectorStateInfo)

	if err != nil {
		return err
	}

	reqData := &buriedmodel.BuriedDataCollectParams{
		DataType: "sector_state",
		Data:     sectorsDataBytes,
	}
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		log.Println(err)
	}

	report.SendReport(reqDataBytes)
	if err != nil {
		return err
	}
	return nil
}
