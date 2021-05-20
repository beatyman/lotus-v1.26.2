package main

import (
	"context"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/filecoin-project/lotus/api/v1api"

	"github.com/filecoin-project/lotus/api/v0api"

	mux "github.com/gorilla/mux"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/buried"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/report"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/proxy"
	"github.com/filecoin-project/lotus/node/repo"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	nauth "github.com/filecoin-project/lotus/node/modules/auth"
	"github.com/gwaylib/errors"
)

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start a lotus miner process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "miner-api",
			Usage: "2345",
		},
		&cli.StringFlag{
			Name:  "report-url",
			Value: "",
			Usage: "report url for state",
		},
		&cli.BoolFlag{
			Name:  "enable-gpu-proving",
			Usage: "enable use of GPU for mining operations",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "timer",
			Usage: "Timer time try. The time is minutes. The default is 30 minutes",
			Value: "30",
		},
		&cli.StringFlag{
			Name:  "storage-collect-interval",
			Usage: "Timer time try. The time is minutes. The default is 30 minutes",
			Value: "30",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Bool("enable-gpu-proving") {
			err := os.Setenv("BELLMAN_NO_GPU", "true")
			if err != nil {
				return err
			}
		}
		minerRepoPath := cctx.String(FlagMinerRepo)
		ctx, _ := tag.New(lcli.DaemonContext(cctx),
			tag.Insert(metrics.Version, build.BuildVersion),
			tag.Insert(metrics.Commit, build.CurrentCommit),
			tag.Insert(metrics.NodeType, "miner"),
		)

		// implement by hlm
		// use the cluster proxy if it's exist.
		if err := cliutil.UseLotusProxy(cctx); err != nil {
			log.Infof("lotus proxy is invalid:%+s", err.Error())
		}
		// init storage database
		// TODO: already implement in init.go, so remove this checking in running?
		database.InitDB(minerRepoPath)
		log.Info("Mount all storage")
		if err := database.ChangeSealedStorageAuth(ctx); err != nil {
			return errors.As(err)
		}
		// mount nfs storage node
		if err := database.MountAllStorage(false); err != nil {
			return errors.As(err)
		}
		log.Info("Clean storage worker")
		// clean storage cur_work cause by no worker on starting.
		if err := database.ClearStorageWork(); err != nil {
			return errors.As(err)
		}
		log.Info("Check sealed")
		// TODO: Move to window post
		// checking sealed for proof
		//if err := ffiwrapper.CheckSealed(minerRepoPath); err != nil {
		//	return errors.As(err)
		//}
		log.Info("Check done")
		// implement by hlm end.

		// Register all metric views
		if err := view.Register(
			metrics.MinerNodeViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}
		// Set the metric to one so it is published to the exporter
		stats.Record(ctx, metrics.LotusInfo.M(1))

		if err := checkV1ApiSupport(ctx, cctx); err != nil {
			return err
		}

		nodeApi, ncloser, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("getting full node api: %w", err)
		}
		defer ncloser()

		v, err := nodeApi.Version(ctx)
		if err != nil {
			return err
		}

		if cctx.Bool("manage-fdlimit") {
			if _, _, err := ulimit.ManageFdLimit(); err != nil {
				log.Errorf("setting file descriptor limit: %s", err)
			}
		}

		if v.APIVersion != api.FullAPIVersion1 {
			return xerrors.Errorf("lotus-daemon API version doesn't match: expected: %s", api.APIVersion{APIVersion: api.FullAPIVersion1})
		}

		log.Info("Checking full node sync status")

		if !cctx.Bool("nosync") {
			if err := lcli.SyncWait(ctx, &v0api.WrapperV1Full{FullNode: nodeApi}, false); err != nil {
				return xerrors.Errorf("sync wait: %w", err)
			}
		}

		r, err := repo.NewFS(minerRepoPath)
		if err != nil {
			return err
		}

		ok, err := r.Exists()
		if err != nil {
			return err
		}
		if !ok {
			return xerrors.Errorf("repo at '%s' is not initialized, run 'lotus-miner init' to set it up", minerRepoPath)
		}

		shutdownChan := make(chan struct{})

		var minerapi api.StorageMiner
		stop, err := node.New(ctx,
			node.StorageMiner(&minerapi),
			node.Override(new(dtypes.ShutdownChan), shutdownChan),
			node.Online(),
			node.Repo(r),

			node.ApplyIf(func(s *node.Settings) bool { return cctx.IsSet("miner-api") },
				node.Override(new(dtypes.APIEndpoint), func() (dtypes.APIEndpoint, error) {
					return multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/" + cctx.String("miner-api"))
				})),
			node.Override(new(v1api.FullNode), nodeApi),
		)
		if err != nil {
			return xerrors.Errorf("creating node: %w", err)
		}

		endpoint, err := r.APIEndpoint()
		if err != nil {
			return xerrors.Errorf("getting API endpoint: %w", err)
		}

		// Bootstrap with full node
		// implement by zhoushuyue
		ok, err = proxy.LotusProxyNetConnect(minerapi.NetConnect)
		if err != nil {
			return errors.As(err)
		}
		// no proxy on, using the local
		// TODO: fix in lotus-miner net connect
		if !ok {
			remoteAddrs, err := nodeApi.NetAddrsListen(ctx)
			if err != nil {
				return xerrors.Errorf("getting full node libp2p address: %w", err)
			}

			if err := minerapi.NetConnect(ctx, remoteAddrs); err != nil {
				return xerrors.Errorf("connecting to full node (libp2p): %w", err)
			}
		}
		// end implement by zhoushuyue

		log.Infof("Remote version %s", v)

		lst, err := manet.Listen(endpoint)
		if err != nil {
			return xerrors.Errorf("could not listen: %w", err)
		}

		mux := mux.NewRouter()

		rpcServer := jsonrpc.NewServer()
		rpcServer.Register("Filecoin", api.PermissionedStorMinerAPI(metrics.MetricedStorMinerAPI(minerapi)))

		mux.Handle("/rpc/v0", rpcServer)
		mux.PathPrefix("/remote").HandlerFunc(minerapi.(*impl.StorageMinerAPI).ServeRemote)
		mux.Handle("/debug/metrics", metrics.Exporter())
		mux.PathPrefix("/").Handler(http.DefaultServeMux) // pprof

		ah := &auth.Handler{
			Verify: minerapi.AuthVerify,
			Next:   mux.ServeHTTP,
		}

		srv := &http.Server{
			Handler: ah,
			BaseContext: func(listener net.Listener) context.Context {
				ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "lotus-miner"))
				return ctx
			},
		}

		// open this rpc for worker.
		scSrv, err := listenSchedulerApi(cctx, r, minerapi.(*impl.StorageMinerAPI))
		if err != nil {
			return errors.As(err)
		}

		sigChan := make(chan os.Signal, 2)
		go func() {
			select {
			case sig := <-sigChan:
				log.Warnw("received shutdown", "signal", sig)
			case <-shutdownChan:
				log.Warn("received shutdown")
			}

			log.Warn("Shutting down...")
			if err := stop(context.TODO()); err != nil {
				log.Errorf("graceful shutting down failed: %s", err)
			}
			if err := scSrv.Shutdown(ctx); err != nil {
				log.Errorf("shutting down scheduler server failed: %s", err)
			}
			if err := srv.Shutdown(context.TODO()); err != nil {
				log.Errorf("shutting down RPC server failed: %s", err)
			}
			log.Warn("Graceful shutdown successful")
		}()
		go func() {
			if err := scSrv.Serve(); err != nil {
				log.Fatal(err)
			}
		}()
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

		if reportUrl := cctx.String("report-url"); len(reportUrl) > 0 {
			report.SetReportUrl(reportUrl)
		}

		log.Info("rebuild tls cert automatic")
		certPath := filepath.Join(minerRepoPath, "miner_crt.pem")
		keyPath := filepath.Join(minerRepoPath, "miner_key.pem")
		if err := nauth.CreateTLSCert(certPath, keyPath); err != nil {
			return errors.As(err)
		}
                // <buried>
                // Collect miner info
		timer := cctx.Int64("timer")
         	go func() {
            		buried.RunCollectMinerInfo(cctx,timer)
         	}()
		interval:=cctx.Int64("storage-collect-interval")        // </buried>
         	go func() {
         		buried.RunCollectStorageNodeStatus(cctx,interval)
		}()
		return srv.ServeTLS(manet.NetListener(lst), certPath, keyPath)
	},
}
