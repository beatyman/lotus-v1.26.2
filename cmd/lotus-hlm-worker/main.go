package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/buried"
	"github.com/filecoin-project/lotus/buried/utils"
	worker2 "github.com/filecoin-project/lotus/buried/worker"
	"github.com/filecoin-project/lotus/lib/tracing"
	"github.com/filecoin-project/lotus/monitor"
	"huangdong2012/filecoin-monitor/model"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/google/gops/agent"
	mux "github.com/gorilla/mux"
	"github.com/gwaylib/errors"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/api.v8/kodocli"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

const (
	QINIU_VIRTUAL_MOUNTPOINT = "/data/oss/qiniu/"
)

var log = logging.Logger("main")

var (
	nodeApi    *api.RetryHlmMinerSchedulerAPI
	nodeCloser jsonrpc.ClientCloser
	nodeCCtx   *cli.Context
	nodeSync   sync.Mutex
)

func closeNodeApi() {
	if nodeCloser != nil {
		nodeCloser()
	}
	nodeApi = nil
	nodeCloser = nil
}

//shutdown之外的地方都需要复用api（断线重连）不能关闭连接
func ReleaseNodeApi(shutdown bool) {
	nodeSync.Lock()
	defer nodeSync.Unlock()
	time.Sleep(3e9)

	if nodeApi == nil {
		return
	}

	if shutdown {
		closeNodeApi()
	}
}

func GetNodeApi() (*api.RetryHlmMinerSchedulerAPI, error) {
	nodeSync.Lock()
	defer nodeSync.Unlock()

	if nodeApi != nil {
		return nodeApi, nil
	}

	nApi, closer, err := lcli.GetHlmMinerSchedulerAPI(nodeCCtx)
	if err != nil {
		closeNodeApi()
		return nil, errors.As(err)
	}
	nodeApi = &api.RetryHlmMinerSchedulerAPI{HlmMinerSchedulerAPI: nApi}
	nodeCloser = closer

	return nodeApi, nil
}

func AuthVerify(ctx context.Context, token string) ([]auth.Permission, error) {
	nApi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}
	return nApi.AuthVerify(ctx, token)
}

func main() {
	lotuslog.SetupLogLevels()

	local := []*cli.Command{
		runCmd,
		rebuildCmd,
		ffiwrapper.AddPieceCmd,
		ffiwrapper.P1Cmd,
		ffiwrapper.P2Cmd,
	}

	app := &cli.App{
		Name:    "lotus-seal-worker",
		Usage:   "Remote storage miner worker",
		Version: build.UserVersion(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "worker-repo",
				EnvVars: []string{"LOTUS_WORKER_PATH", "WORKER_PATH"},
				Value:   "~/.lotusworker", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "miner-repo",
				EnvVars: []string{"LOTUS_MINER_PATH", "LOTUS_STORAGE_PATH"},
				Value:   "~/.lotusstorage", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "storage-repo",
				EnvVars: []string{"WORKER_SEALED_PATH"},
				Value:   "~/.lotusworker/push", // TODO: Consider XDG_DATA_HOME
			},
		},

		Commands: local,
	}
	app.Setup()
	app.Metadata["repoType"] = repo.StorageMiner

	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start lotus worker",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "id-file",
			EnvVars: []string{"WORKER_ID_PATH"},
			Value:   "~/.lotusworker/worker.id", // TODO: Consider XDG_DATA_HOME
		},
		&cli.StringFlag{
			Name:  "report-url",
			Value: "",
			Usage: "report url for state, TODO: remove this field",
		},
		&cli.StringFlag{
			Name:  "listen-addr",
			Value: "",
			Usage: "listen address a local",
		},
		&cli.StringFlag{
			Name:  "server-addr",
			Value: "",
			Usage: "server address for visit",
		},
		&cli.UintFlag{
			Name:  "max-tasks",
			Value: 1,
			Usage: "max tasks for memory.",
		},
		&cli.UintFlag{
			Name:  "transfer-buffer",
			Value: 1,
			Usage: "Number of sectors per disk which in transfering",
		},
		&cli.UintFlag{
			Name:  "cache-mode",
			Value: 0,
			Usage: "cache mode. 0, tranfer mode; 1, share mode",
		},
		&cli.UintFlag{
			Name:    "parallel-pledge",
			Aliases: []string{"parallel-addpiece"},
			Value:   1,
			Usage:   "Parallel of pledge, num <= max-tasks",
		},
		&cli.UintFlag{
			Name:  "parallel-precommit1",
			Value: 1,
			Usage: "Parallel of precommit1, num <= max-tasks",
		},
		&cli.UintFlag{
			Name:  "parallel-precommit2",
			Value: 1,
			Usage: "Parallel of precommit2, num <= max-tasks",
		},
		&cli.UintFlag{
			Name:  "parallel-commit",
			Value: 1,
			Usage: "Parallel of commit, num <= max-tasks. if the number is 0, will select a commit service until success",
		},
		&cli.BoolFlag{
			Name:  "commit2-srv",
			Value: false,
			Usage: "Open the commit2 service",
		},
		&cli.BoolFlag{
			Name:  "wdpost-srv",
			Value: false,
			Usage: "Open window PoSt service",
		},
		&cli.BoolFlag{
			Name:  "wnpost-srv",
			Value: false,
			Usage: "Open wining PoSt service",
		},
		&cli.StringFlag{
			Name:  "timer",
			Usage: "Timer time try. The time is minutes. The default is 1 minutes",
			Value: "1",
		},
		&cli.StringFlag{
			Name:  "worker-conf-yml",
			Usage: "Timer time try. The time is minutes. The default is 1 minutes",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Starting lotus worker")
		//start gops
		if err := agent.Listen(agent.Options{}); err != nil {
			return err
		}

		//init env for rust
		{
			if err := os.Setenv("Lotus_Parallel_AddPiece", fmt.Sprintf("%v", cctx.Uint("parallel-addpiece"))); err != nil {
				return err
			}
			if err := os.Setenv("Lotus_Parallel_PreCommit1", fmt.Sprintf("%v", cctx.Uint("parallel-precommit1"))); err != nil {
				return err
			}
			if err := os.Setenv("Lotus_Parallel_PreCommit2", fmt.Sprintf("%v", cctx.Uint("parallel-precommit2"))); err != nil {
				return err
			}
			if err := os.Setenv("Lotus_Parallel_Commit", fmt.Sprintf("%v", cctx.Uint("parallel-commit"))); err != nil {
				return err
			}
		}
		nodeCCtx = cctx

		nodeApi, err := GetNodeApi()
		if err != nil {
			return xerrors.Errorf("getting miner api: %w", err)
		}
		defer ReleaseNodeApi(true)
		defer ffiwrapper.FreeAllTaskPid()

		log.Infof("getting ainfo for StorageMiner")
		ainfo, err := lcli.GetHlmMinerSchedulerAPIInfo(cctx)
		if err != nil {
			return xerrors.Errorf("could not get api info: %w", err)
		}
		minerAddr, err := ainfo.Host()
		if err != nil {
			return err
		}

		workerRepo, err := homedir.Expand(cctx.String("worker-repo"))
		if err != nil {
			return err
		}

		minerRepo, err := homedir.Expand(cctx.String("miner-repo"))
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(lcli.ReqContext(nodeCCtx))
		defer cancel()

		apitemp, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		ss, err := apitemp.PreStorageNode(ctx, "", "", database.STORAGE_KIND_SEALED)
		if err != nil {
			return errors.As(err)
		}
		if ss.MountType == database.MOUNT_TYPE_OSS {
			cctx.Set("storage-repo", QINIU_VIRTUAL_MOUNTPOINT)
		}
		sealedRepo, err := homedir.Expand(cctx.String("storage-repo"))
		if err != nil {
			return err
		}

		idFile := cctx.String("id-file")
		if len(idFile) == 0 {
			idFile = "~/.lotusworker/worker.id"
		}
		workerIdFile, err := homedir.Expand(idFile)
		if err != nil {
			return err
		}

		log.Infof("getting miner actor")
		act, err := nodeApi.ActorAddress(ctx)
		if err != nil {
			return err
		}

		log.Infof("getting worker actor")
		workerAddr, err := nodeApi.WorkerAddress(ctx, act, types.EmptyTSK)
		if err != nil {
			return errors.As(err)
		}

		if err := os.MkdirAll(workerRepo, 0755); err != nil {
			return errors.As(err, workerRepo)
		}

		minerSealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: minerRepo,
		})
		if err != nil {
			return err
		}
		if err := os.MkdirAll(sealedRepo, 0755); err != nil {
			return errors.As(err, sealedRepo)
		}
		workerId := GetWorkerID(workerIdFile)
		netIp := os.Getenv("NETIP")
		// make serivce server
		listenAddr := cctx.String("listen-addr")
		if len(listenAddr) == 0 {
			listenAddr = netIp + ":1280"
		}
		serverAddr := cctx.String("server-addr")
		if len(serverAddr) == 0 {
			serverAddr = listenAddr
		}

		log.Info("umounting history sealed")
		// init and clean the sealed dir who has mounted.
		sealedMountedCfg := workerIdFile + ".lock"
		if err := umountAllRemote(sealedMountedCfg); err != nil {
			return errors.As(err)
		}

		// init worker configuration
		//workerCfg := ffiwrapper.WorkerCfg{
		//	ID:                 workerId,
		//	IP:                 netIp,
		//	SvcUri:             serverAddr,
		//	MaxTaskNum:         int(cctx.Uint("max-tasks")),
		//	CacheMode:          int(cctx.Uint("cache-mode")),
		//	TransferBuffer:     int(cctx.Uint("transfer-buffer")),
		//	ParallelPledge:     int(cctx.Uint("parallel-addpiece")),
		//	ParallelPrecommit1: int(cctx.Uint("parallel-precommit1")),
		//	ParallelPrecommit2: int(cctx.Uint("parallel-precommit2")),
		//	ParallelCommit:     int(cctx.Uint("parallel-commit")),
		//	Commit2Srv:         cctx.Bool("commit2-srv"),
		//	WdPoStSrv:          cctx.Bool("wdpost-srv"),
		//	WnPoStSrv:          cctx.Bool("wnpost-srv"),
		//}
		workerConfYml := cctx.String("worker-conf-yml")

		if len(workerConfYml) > 0 {
			worker2.WORKER_WATCH_FILE = workerConfYml
		}
		workerCfg := worker2.GetConfigWorker(cctx, workerId, netIp, serverAddr)
		cfgbyte, _ := json.Marshal(workerCfg)
		log.Info(string(cfgbyte), "=============================22222222222")
		workerApi := newRpcServer(workerCfg.ID, minerRepo, minerSealer)

		minerNo := utils.GetMinerAddr()
		log.Infof("==============get addr :%s", minerNo)

		go func() {
			worker2.InitWatch(ctx, workerId, nodeApi)
		}()
		//添加监控
		kind := model.PackageKind_Worker
		if workerCfg.WdPoStSrv {
			kind = model.PackageKind_WorkerWdPost
		} else if workerCfg.WnPoStSrv {
			kind = model.PackageKind_WorkerWnPost
		}
		monitor.Init(kind, minerNo)
		jaeger := tracing.SetupJaegerTracing("worker")
		defer func() {
			if jaeger != nil {
				jaeger.Flush()
			}
		}()

		timer := cctx.Int64("timer")
		go func() {
			buried.RunCollectWorkerInfo(cctx, timer, workerCfg, minerNo)
		}()

		if err := database.LockMount(minerRepo); err != nil {
			log.Infof("mount lock failed, skip mount the storages:%s", errors.As(err, minerRepo).Code())
		}
		defer database.UnlockMount(minerRepo)

		if workerCfg.WdPoStSrv || workerCfg.WnPoStSrv {
			if err := workerApi.loadMinerStorage(ctx, nodeApi); err != nil {
				return errors.As(err)
			}
		}

		mux := mux.NewRouter()
		rpcServer := jsonrpc.NewServer()
		rpcServer.Register("Filecoin", api.PermissionedWorkerHlmAPI(workerApi))
		mux.Handle("/rpc/v0", rpcServer)
		mux.PathPrefix("/file").HandlerFunc(fileserver.NewStorageFileServer(workerRepo).FileHttpServer)
		ah := &auth.Handler{
			Verify: AuthVerify,
			Next:   mux.ServeHTTP,
		}
		srv := &http.Server{
			Handler: ah,
			BaseContext: func(listener net.Listener) context.Context {
				return ctx
			},
		}
		nl, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return err
		}

		log.Infof("starting serve :%s", listenAddr)
		go func() {
			if err := srv.Serve(nl); err != nil {
				log.Error(err)
				os.Exit(1)
			}
		}()
		//设置日志级别
		elog := kodocli.NewLogger()
		elog.SetLevel(kodocli.LOG_LEVEL_ERROR)
		operation.SetLogger(elog)
		log.Info("starting acceptJobs")
		if err := acceptJobs(ctx,
			workerApi,
			act, workerAddr,
			minerAddr, ainfo.AuthHeader(),
			workerRepo, sealedRepo, sealedMountedCfg,
			workerCfg,
		); err != nil {
			if err := umountAllRemote(sealedMountedCfg); err != nil {
				log.Warn(errors.As(err))
			}

			log.Warn(err)
			ReleaseNodeApi(true)
		}
		if err := srv.Shutdown(ctx); err != nil {
			log.Errorf("shutting down RPC server failed: %s", err)
		}
		log.Info("worker exit")
		return nil
	},
}
