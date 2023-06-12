package worker

import (
	"context"
	"encoding/json"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"reflect"
)

var WORKER_WATCH_DIR = "../../etc"
var WORKER_WATCH_FILE = "../../etc/worker.yml"
var FIEXED_ENV = "{\"IPFS_GATEWAY\":\"https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/\",\"FIL_PROOFS_USE_GPU_COLUMN_BUILDER\":\"1\",\"FIL_PROOFS_USE_GPU_TREE_BUILDER\":\"1\",\"FIL_PROOFS_MAXIMIZE_CACHING\":\"1\",\"FIL_PROOFS_USE_MULTICORE_SDR\":\"1\",\"FIL_PROOFS_PARENT_CACHE\":\"/data/cache/filecoin-parents\",\"FIL_PROOFS_PARAMETER_CACHE\":\"/data/cache/filecoin-proof-parameters/v28\",\"RUST_LOG\":\"info\",\"RUST_BACKTRACE\":\"1\"}"
var ENVIRONMENT_VARIABLE = "{\"ENABLE_COPY_MERKLE_TREE\":\"1\",\"US3\":\"\"}"

var CFG_WORKER = model.WorkerConf{}

func InitWatch(ctx context.Context, workerId string, napi *api.RetryHlmMinerSchedulerAPI) chan bool {
	log.Info("=================")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()
	done := make(chan bool)
	err = watcher.Add(WORKER_WATCH_DIR)
	if err != nil {
		log.Fatal(err)
	}
	go Watch(ctx, watcher, workerId, napi)

	<-done
	return done
}

func Watch(ctx context.Context, watcher *fsnotify.Watcher, workerId string, napi *api.RetryHlmMinerSchedulerAPI) {
	for {
		select {
		case event, ok := <-watcher.Events:
			log.Info(event.Op.String(), "===========", event.Name, "=========", WORKER_WATCH_FILE)
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if event.Name == WORKER_WATCH_FILE {
					t := model.WorkerConf{}
					//把yaml形式的字符串解析成struct类型
					//读取文件
					data, err := ioutil.ReadFile(WORKER_WATCH_FILE)
					if err != nil {
						log.Error("Read_File_Err_:", err.Error())
						t = model.WorkerConf{
							ID: workerId,
						}
					}
					err = yaml.Unmarshal(data, &t)
					if err != nil {
						log.Error("Read_File_Err_yml:", err.Error())
						t = model.WorkerConf{
							ID: workerId,
						}
					}
					if err != nil {
						log.Error("Read_File_Err_Worker_Conf_:", err.Error())
						t = model.WorkerConf{
							ID: workerId,
						}
					}
					//修改struct里面的记录
					if len(t.FixedEnv) > 0 {
						var m map[string]string
						json.Unmarshal([]byte(t.FixedEnv), &m)
						for k, v := range m {
							os.Setenv(k, v)
							log.Info("=========key========", k, "=============value========", v, "============os.getenv", os.Getenv(k))
						}
					}
					if len(t.EnvironmentVariable) > 0 {
						var m map[string]string
						json.Unmarshal([]byte(t.EnvironmentVariable), &m)
						for k, v := range m {
							os.Setenv(k, v)
							log.Info("=========key========", k, "=============value========", v, "============os.getenv", os.Getenv(k))
						}
					}
					var json1 = ffiwrapper.WorkerCfg{
						ID:                 t.ID,
						IP:                 t.IP,
						SvcUri:             t.SvcUri,
						MaxTaskNum:         t.MaxTaskNum,
						ParallelPledge:     t.ParallelPledge,
						ParallelPrecommit1: t.ParallelPrecommit1,
						ParallelPrecommit2: t.ParallelPrecommit2,
						ParallelCommit:     t.ParallelCommit,
						Commit2Srv:         t.Commit2Srv,
						WdPoStSrv:          t.WdPoStSrv,
						WnPoStSrv:          t.WnPoStSrv,
					}
					log.Info("==========1=111111111111111=1", CFG_WORKER)
					json1.ID = workerId
					if !reflect.DeepEqual(CFG_WORKER, t) {
						CFG_WORKER = t
						//发送api
						err := napi.WorkerFileWatch(ctx, json1)
						if err != nil {
							log.Error("================WorkerFileWatch_ERR=========", err.Error())
						}
						log.Info("==========2=222222222222=2", CFG_WORKER)
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Info("error:", err)
		}
	}
}
