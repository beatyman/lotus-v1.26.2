package buried

import (
	"context"
	"encoding/json"
	"github.com/filecoin-project/lotus/buried/miner"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/buried/utils"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/gwaylib/log"
	"github.com/shirou/gopsutil/host"
	"github.com/urfave/cli/v2"
	"huangdong2012/filecoin-monitor/trace/spans"
	"time"
)

// RunCollectMinerInfo :
func RunCollectMinerInfo(cctx *cli.Context, timer int64) chan bool {
	ticker := time.NewTicker(time.Duration(timer*60) * time.Second)
	quit := make(chan bool, 1)

	go func() {
		for {
			select {
			case <-ticker.C:
				minerInfo, err := miner.CollectMinerInfo(cctx)
				if err != nil {
					log.Error(err)
					continue
				}
				minerInfoDataBytes, err := json.Marshal(minerInfo)
				if err != nil {
					log.Error(err)
					continue
				}
				reqData := &buriedmodel.BuriedDataCollectParams{
					DataType: "miner_info",
					Data:     minerInfoDataBytes,
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
					continue
				}
				go report.SendReport(kafaRestDataBytes)
			case <-quit:
				ticker.Stop()
			}
		}
	}()

	return quit
}

func RunCollectWorkerInfo(cctx *cli.Context, timer int64, workerCfg ffiwrapper.WorkerCfg, minerId string) chan bool {
	ticker := time.NewTicker(time.Duration(timer*60) * time.Second)
	quit := make(chan bool, 1)

	go func() {
		for {
			select {
			case <-ticker.C:
				//nodeApi1, closer, err := lcli.GetStorageMinerAPI(cctx)
				//if err != nil {
				//	log.Error("==========================================", err)
				//	return
				//}
				//info, err := nodeApi1.WorkerInfo(context.Background(), workerCfg.ID)
				//if err != nil {
				//	log.Error("==========================================", err)
				//	return
				//}
				//log.Info("==========================================11111111", info)
				hostInfo, _ := host.Info()
				ip4, _ := utils.GetLocalIP()
				versionStr := utils.ExeSysCommand("./lotus-worker -v")
				var workerInfo = buriedmodel.WorkerInfo{}
				var nodeInfo = buriedmodel.NodeInfo{
					HostNo:  hostInfo.HostID,
					HostIP:  ip4,
					Status:  buriedmodel.NodeStatus_Online,
					Version: versionStr,
				}
				//data, err := io.ReadFile(worker.WORKER_WATCH_FILE)
				//if err != nil {
				//	log.Error("Read_File_Err_:", err.Error())
				//}
				//var json1 = ffiwrapper.WorkerCfg{}
				//err = json.Unmarshal(data, &json1)
				//if err != nil {
				//	log.Error("worker_report_read_file_error : ", err.Error())
				//	return
				//}
				workerInfo.WorkerNo = workerCfg.ID
				workerInfo.MinerId = minerId
				workerInfo.SvcUri = workerCfg.SvcUri
				workerInfo.MaxTaskNum = workerCfg.MaxTaskNum
				workerInfo.ParallelPledge = workerCfg.ParallelPledge
				workerInfo.ParallelPrecommit1 = workerCfg.ParallelPrecommit1
				workerInfo.ParallelPrecommit2 = workerCfg.ParallelPrecommit2
				workerInfo.ParallelCommit = workerCfg.ParallelCommit
				workerInfo.Commit2Srv = workerCfg.Commit2Srv
				workerInfo.WdPostSrv = workerCfg.WdPoStSrv
				workerInfo.WnPostSrv = workerCfg.WnPoStSrv
				//workerInfo.Disable = info.Disable
				//格式化json
				workerInfo.NodeInfo = &nodeInfo
				workerInfoStr, _ := json.Marshal(workerInfo)
				//log.Info(string(workerInfoStr))
				_, span := spans.NewWorkerSpan(context.Background())
				span.SetInfo(string(workerInfoStr))
				span.End()
				//				closer()
			case <-quit:
				ticker.Stop()
			}
		}
	}()

	return quit
}

//monitor storage server status
func RunCollectStorageNodeStatus(cctx *cli.Context, timer int64) chan bool {
	ticker := time.NewTicker(time.Duration(timer*60) * time.Second)
	quit := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-ticker.C:
				nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
				if err != nil {
					log.Error(err)
					continue
				}
				defer closer()
				ctx := lcli.ReqContext(cctx)
				// todo "SELECT tb1.id, tb1.mount_dir, tb1.mount_signal_uri, disable FROM storage_info tb1 LIMIT 10000"
				stats, err := nodeApi.StatusHLMStorage(ctx, 0, time.Duration(30)*time.Second)
				if err != nil {
					log.Error(err)
					continue
				}
				//todo SELECT * FROM storage_info
				infos, err := database.GetAllStorageInfoAll()
				if err != nil {
					log.Error(err)
					continue
				}
				type StorageInfo struct {
					Status  []database.StorageStatus `json:"status"`
					Infos   []database.StorageInfo   `json:"infos"`
					MinerId string                   `json:"miner_id"`
				}
				minerInfo, err := miner.CollectMinerInfo(cctx)
				if err != nil {
					log.Error(err)
					continue
				}

				info := StorageInfo{
					Status:  stats,
					Infos:   infos,
					MinerId: minerInfo.MinerID,
				}
				storageInfoDataBytes, err := json.Marshal(info)
				if err != nil {
					log.Error(err)
					continue

				}
				reqData := &buriedmodel.BuriedDataCollectParams{
					DataType: "storage_info",
					Data:     storageInfoDataBytes,
				}
				records := make(map[string]interface{})
				records["records"] = []map[string]interface{}{
					{"value": reqData},
				}

				reqDataBytes, err := json.Marshal(records)
				go report.SendReport(reqDataBytes)
			case <-quit:
				ticker.Stop()
			}
		}
	}()

	return quit
}
