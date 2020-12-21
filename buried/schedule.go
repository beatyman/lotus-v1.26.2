package buried

import (
	"encoding/json"
	"time"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/filecoin-project/lotus/buried/miner"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/gwaylib/log"
	"github.com/urfave/cli/v2"
)

// RunCollectMinerInfo :
func RunCollectMinerInfo(cctx *cli.Context) chan bool {
	ticker := time.NewTicker(60 * time.Second)
	quit := make(chan bool, 1)

	go func() {
		for {
			select {
			case <-ticker.C:
				log.Info("Time to collect miner info > > >")
				minerInfo, err := miner.CollectMinerInfo(cctx)
				log.Errorf("============----------------==============",err)
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
				log.Errorf("=================================--------------=====================%v",minerInfo)
				reqDataBytes, err := json.Marshal(reqData)
				report.SendReport(reqDataBytes)
			case <-quit:
				ticker.Stop()
			}
		}
	}()

	return quit
}

