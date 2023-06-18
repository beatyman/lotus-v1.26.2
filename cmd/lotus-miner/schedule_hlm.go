package main

import (
	"context"
	"github.com/filecoin-project/lotus/metrics"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/fileserver"
	mproxy "github.com/filecoin-project/lotus/metrics/proxy"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/repo"
	mux "github.com/gorilla/mux"
	"github.com/gwaylib/errors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/tag"
)

type SchedulerServer struct {
	srv *http.Server
	l   net.Listener
}

func (s *SchedulerServer) Serve() error {
	return s.srv.Serve(s.l)
}
func (s *SchedulerServer) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func listenSchedulerApi(cctx *cli.Context, repoFs *repo.FsRepo, sm *impl.StorageMinerAPI) (*SchedulerServer, error) {
	defCfg := config.DefaultStorageMiner()
	cfgI, err := config.FromFile(repoFs.ConfigPath(), config.SetDefault(func() (interface{}, error) { return defCfg, nil }))
	if err != nil {
		return nil, err
	}

	cfg, ok := cfgI.(*config.StorageMiner)
	if !ok {
		return nil, errors.New("not *config.StorageMiner")
	}
	strma := strings.TrimSpace(cfg.WorkerAPI.ListenAddress)
	apima, err := multiaddr.NewMultiaddr(strma)
	if err != nil {
		return nil, errors.As(err, strma)
	}

	lst, err := manet.Listen(apima)
	if err != nil {
		return nil, errors.As(err)
	}

	mux := mux.NewRouter()
	hs := impl.NewHlmMinerScheduler(sm)
	rpcServer := jsonrpc.NewServer()
	rpcServer.Register("Filecoin", api.PermissionedHlmMinerSchedulerAPI(mproxy.MetricedHlmMinerSchedulerAPI(hs)))
	mux.Handle("/rpc/v0", rpcServer)
	mux.PathPrefix("/file").HandlerFunc(fileserver.NewStorageFileServer(repoFs.Path()).FileHttpServer)

	ah := &auth.Handler{
		Verify: hs.AuthVerify,
		Next:   mux.ServeHTTP,
	}

	srv := &http.Server{
		Handler: ah,
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(lcli.DaemonContext(cctx),
				tag.Insert(metrics.Version, build.BuildVersion),
				tag.Insert(metrics.Commit, build.CurrentCommit),
				tag.Insert(metrics.NodeType, "miner"),
			)
			return ctx
		},
	}

	if err := ioutil.WriteFile(filepath.Join(repoFs.Path(), "worker_api"), []byte(cfg.WorkerAPI.ListenAddress), 0600); err != nil {
		return nil, err
	}
	return &SchedulerServer{
		srv: srv,
		l:   manet.NetListener(lst),
	}, nil
}
