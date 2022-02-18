package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/gwaylib/log"
	"github.com/urfave/cli/v2"
	io "io/ioutil"
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

	kafkaRestValue := buriedmodel.KafkaRestValue{
		Value: reqData,
	}

	var values []buriedmodel.KafkaRestValue
	values = append(values, kafkaRestValue)

	kafaRestData := &buriedmodel.KafkaRestData{
		Records: values,
	}
	kafaRestDataBytes, err := json.Marshal(kafaRestData)
	if err != nil {
		log.Error(err)
		return err
	}
	report.SendRpcReport(kafaRestDataBytes)
	//go rpcclient.Send(kafaRestDataBytes)
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
		MinerID:  task.SectorStorage.SectorInfo.MinerId,
		WorkerID: task.WorkerID,
		ClientIP: task.SectorStorage.WorkerInfo.Ip,
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

	go report.SendReport(reqDataBytes)
	//go rpcclient.Send(reqDataBytes)
	if err != nil {
		return err
	}
	return nil
}

func GetConfigWorker(cctx *cli.Context, workerId string, netIp string, serverAddr string) ffiwrapper.WorkerCfg {
	workerCfg := ffiwrapper.WorkerCfg{
		ID:                 workerId,
		IP:                 netIp,
		SvcUri:             serverAddr,
		MaxTaskNum:         int(cctx.Uint("max-tasks")),
		CacheMode:          int(cctx.Uint("cache-mode")),
		TransferBuffer:     int(cctx.Uint("transfer-buffer")),
		ParallelPledge:     int(cctx.Uint("parallel-addpiece")),
		ParallelPrecommit1: int(cctx.Uint("parallel-precommit1")),
		ParallelPrecommit2: int(cctx.Uint("parallel-precommit2")),
		ParallelCommit:     int(cctx.Uint("parallel-commit")),
		Commit2Srv:         cctx.Bool("commit2-srv"),
		WdPoStSrv:          cctx.Bool("wdpost-srv"),
		WnPoStSrv:          cctx.Bool("wnpost-srv"),
	}
	//判断文件是否存在，如果存在则使用文件里面的任务数，如果不存在，则使用第一次配置的。
	isExist, err := ffiwrapper.PathExists(WORKER_WATCH_FILE)
	log.Info("=============worker-watch_file===========", isExist)
	if err != nil {
		log.Errorf("read_worker-watch_file_err: %s", err)
		return workerCfg
	}
	var json1 = buriedmodel.WorkerInfoCfg{}
	if isExist {
		//读取文件，判断id是否一致，不一致则修改id
		data, err := io.ReadFile(WORKER_WATCH_FILE)
		if err != nil {
			log.Error("Read_File_Err_:", err.Error())
			return workerCfg
		}
		err = json.Unmarshal(data, &json1)
		if err != nil {
			log.Error("json_Read_File_Err_:", err.Error())
			return workerCfg
		}
		//todo 讨论处理， 是否一台机器起一个worker? 还是根据worker进程来判断文件
		if json1.ID != workerId {

		}
		workerCfg.MaxTaskNum = json1.MaxTaskNum
		workerCfg.ParallelPledge = json1.ParallelPledge
		workerCfg.ParallelPrecommit1 = json1.ParallelPrecommit1
		workerCfg.ParallelPrecommit2 = json1.ParallelPrecommit2
		workerCfg.ParallelCommit = json1.ParallelCommit
		workerCfg.Commit2Srv = json1.Commit2Srv
		workerCfg.WdPoStSrv = json1.WdPoStSrv
		workerCfg.WnPoStSrv = json1.WnPoStSrv
		json1.SvcUri = workerCfg.SvcUri
		json1.ID = workerCfg.ID
	} else {
		// init worker configuration
		json1.MaxTaskNum = workerCfg.MaxTaskNum
		json1.ParallelPledge = workerCfg.ParallelPledge
		json1.ParallelPrecommit1 = workerCfg.ParallelPrecommit1
		json1.ParallelPrecommit2 = workerCfg.ParallelPrecommit2
		json1.ParallelCommit = workerCfg.ParallelCommit
		json1.Commit2Srv = workerCfg.Commit2Srv
		json1.WdPoStSrv = workerCfg.WdPoStSrv
		json1.WnPoStSrv = workerCfg.WnPoStSrv
		json1.ID = workerCfg.ID
		json1.IP = workerCfg.IP
		json1.SvcUri = workerCfg.SvcUri
	}
	var str bytes.Buffer
	byte_json, _ := json.Marshal(json1)
	_ = json.Indent(&str, byte_json, "", "    ")
	var d1 = []byte(str.String())
	err2 := io.WriteFile(WORKER_WATCH_FILE, d1, 0666) //写入文件(字节数组)
	if err2 != nil {
		fmt.Errorf(err2.Error())
	}
	return workerCfg
}
