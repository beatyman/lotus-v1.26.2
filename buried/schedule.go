package buried

import (
	"encoding/json"
	"github.com/filecoin-project/lotus/buried/miner"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/gwaylib/log"
	"github.com/urfave/cli/v2"
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
