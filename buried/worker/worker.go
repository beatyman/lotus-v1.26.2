package worker

import (
	"bytes"
	"encoding/json"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"
	io "io/ioutil"
	"os"
	"strings"
)

var log = logging.Logger("worker")

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
		log.Error(err)
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
	log.Info("=========================WORKER_WATCH_FILE====================", WORKER_WATCH_FILE)
	isExist, err := ffiwrapper.PathExists(WORKER_WATCH_FILE)
	log.Info("=============worker-watch_file===========", isExist)
	if err != nil {
		log.Errorf("read_worker-watch_file_err: %s", err)
		return workerCfg
	}
	t := buriedmodel.WorkerConf{}
	if isExist {
		//把yaml形式的字符串解析成struct类型
		//读取文件
		data, err := io.ReadFile(WORKER_WATCH_FILE)
		if err != nil {
			log.Error("Read_File_Err_yml:", err.Error())
			t = buriedmodel.WorkerConf{
				ID:     workerId,
				IP:     netIp,
				SvcUri: serverAddr,
			}
		}
		err = yaml.Unmarshal(data, &t)
		if err != nil {
			log.Error("Read_File_Err_Worker_Conf_:", err.Error())
			t = buriedmodel.WorkerConf{
				ID:     workerId,
				IP:     netIp,
				SvcUri: serverAddr,
			}
		}
		workerCfg.MaxTaskNum = t.MaxTaskNum
		workerCfg.ParallelPledge = t.ParallelPledge
		workerCfg.ParallelPrecommit1 = t.ParallelPrecommit1
		workerCfg.ParallelPrecommit2 = t.ParallelPrecommit2
		workerCfg.ParallelCommit = t.ParallelCommit
		workerCfg.Commit2Srv = t.Commit2Srv
		workerCfg.WdPoStSrv = t.WdPoStSrv
		workerCfg.WnPoStSrv = t.WnPoStSrv
		t.SvcUri = workerCfg.SvcUri
		t.IP = netIp
		t.ID = workerCfg.ID
	} else {
		// init worker configuration
		t.MaxTaskNum = workerCfg.MaxTaskNum
		t.ParallelPledge = workerCfg.ParallelPledge
		t.ParallelPrecommit1 = workerCfg.ParallelPrecommit1
		t.ParallelPrecommit2 = workerCfg.ParallelPrecommit2
		t.ParallelCommit = workerCfg.ParallelCommit
		t.Commit2Srv = workerCfg.Commit2Srv
		t.WdPoStSrv = workerCfg.WdPoStSrv
		t.WnPoStSrv = workerCfg.WnPoStSrv
		t.ID = workerCfg.ID
		t.IP = workerCfg.IP
		t.SvcUri = workerCfg.SvcUri
		t.AutoInstall = false
		var str bytes.Buffer
		_ = json.Indent(&str, []byte(ENVIRONMENT_VARIABLE), "", "    ")
		t.EnvironmentVariable = str.String()
		str.Reset()
		_ = json.Indent(&str, []byte(FIEXED_ENV), "", "    ")
		t.FixedEnv = str.String()
	}
	if len(t.FixedEnv) > 0 {
		var m map[string]string
		json.Unmarshal([]byte(t.FixedEnv), &m)
		for k, v := range m {
			os.Setenv(k, v)
		}
	}
	if len(t.EnvironmentVariable) > 0 {
		var m map[string]string
		json.Unmarshal([]byte(t.EnvironmentVariable), &m)
		for k, v := range m {
			os.Setenv(k, v)
		}
	}

	d, err := yaml.Marshal(&t)
	CFG_WORKER = t
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	err2 := io.WriteFile(WORKER_WATCH_FILE, d, 0666) //写入文件(字节数组)
	if err2 != nil {
		log.Errorf(err2.Error())
	}
	return workerCfg
}
