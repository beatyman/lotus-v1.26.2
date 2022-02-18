package worker

import (
	"context"
	"encoding/json"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/fsnotify/fsnotify"
	"github.com/gwaylib/log"
	io "io/ioutil"
	"reflect"
)

var WORKER_WATCH_DIR = "../../etc"
var WORKER_WATCH_FILE = "../../etc/worker_file.json"

var cfg_worker = ffiwrapper.WorkerCfg{}

//func Watch(watcher *fsnotify.Watcher) {
//	log.Info("===================worker_watch=================================")
//	for {
//		select {
//		case event, ok := <-watcher.Events:
//			if !ok {
//				return
//			}
//			if event.Op&fsnotify.Write == fsnotify.Write {
//				data, err := io.ReadFile(WORKER_WATCH_FILE)
//				if err != nil {
//					panic(err)
//				}
//				var json1 = ffiwrapper.WorkerCfg{}
//				err = json.Unmarshal(data, &json1)
//				if !reflect.DeepEqual(cfg_worker, json1) {
//					cfg_worker = json1
//					log.Info(cfg_worker, "================3333333333333333======================˚")
//				}
//			}
//
//		case err, ok := <-watcher.Errors:
//			if !ok {
//				return
//			}
//			log.Println("error:", err)
//		}
//	}
//}

func InitWorkerWatch() chan bool {
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watch.Close()
	//添加要监控的对象，文件或文件夹
	err = watch.Add(WORKER_WATCH_DIR)
	if err != nil {
		log.Fatal(err)
	}
	//我们另启一个goroutine来处理监控对象的事件
	go func() {
		for {
			select {
			case ev := <-watch.Events:
				{
					//判断事件发生的类型，如下5种
					// Create 创建
					// Write 写入
					// Remove 删除
					// Rename 重命名
					// Chmod 修改权限
					if ev.Op&fsnotify.Create == fsnotify.Create {
						log.Println("创建文件 : ", ev.Name)
					}
					if ev.Op&fsnotify.Write == fsnotify.Write {
						log.Println("写入文件 : ", ev.Name)
					}
					if ev.Op&fsnotify.Remove == fsnotify.Remove {
						log.Println("删除文件 : ", ev.Name)
					}
					if ev.Op&fsnotify.Rename == fsnotify.Rename {
						log.Println("重命名文件 : ", ev.Name)
					}
					if ev.Op&fsnotify.Chmod == fsnotify.Chmod {
						log.Println("修改权限 : ", ev.Name)
					}
				}
			case err := <-watch.Errors:
				{
					log.Println("error : ", err)
					return
				}
			}
		}
	}()
	done := make(chan bool)
	return done
}

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
			log.Println(event.Op.String(), "===========", event.Name)
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if event.Name == WORKER_WATCH_FILE {
					data, err := io.ReadFile(WORKER_WATCH_FILE)
					if err != nil {
						log.Error("Read_File_Err_:", err.Error())
					}
					var json1 = ffiwrapper.WorkerCfg{}
					err = json.Unmarshal(data, &json1)
					log.Info("==========1=111111111111111=1", cfg_worker)
					json1.ID = workerId
					if !reflect.DeepEqual(cfg_worker, json1) {
						cfg_worker = json1
						//发送api
						err := napi.WorkerFileWatch(ctx, cfg_worker)
						if err != nil {
							log.Error("================WorkerFileWatch_ERR=========", err.Error())
						}
						log.Info("==========2=222222222222=2", cfg_worker)
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("error:", err)
		}
	}
}
