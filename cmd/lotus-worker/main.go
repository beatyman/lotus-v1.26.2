package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	paramfetch "github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/sector-storage/ffiwrapper/basicfs"
	mux "github.com/gorilla/mux"
	"github.com/mitchellh/go-homedir"

	"github.com/filecoin-project/go-jsonrpc/auth"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	manet "github.com/multiformats/go-multiaddr-net"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/filecoin-project/lotus/node/repo"

	"github.com/gwaylib/errors"
)

var log = logging.Logger("main")

var (
	nodeApi    api.StorageMiner
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

func ReleaseNodeApi(shutdown bool) {
	nodeSync.Lock()
	defer nodeSync.Unlock()
	time.Sleep(3e9)

	if nodeApi == nil {
		return
	}

	if shutdown {
		closeNodeApi()
		return
	}

	ctx := lcli.ReqContext(nodeCCtx)

	// try reconnection
	_, err := nodeApi.Version(ctx)
	if err != nil {
		closeNodeApi()
		return
	}
}

func GetNodeApi() (api.StorageMiner, error) {
	nodeSync.Lock()
	defer nodeSync.Unlock()

	if nodeApi != nil {
		return nodeApi, nil
	}

	ctx := lcli.ReqContext(nodeCCtx)
	if nodeCCtx == nil {
		panic("need init node cctx")
	}

	nApi, closer, err := lcli.GetStorageMinerAPI(nodeCCtx)
	if err != nil {
		closeNodeApi()
		return nil, errors.As(err)
	}
	nodeApi = nApi
	nodeCloser = closer

	v, err := nodeApi.Version(ctx)
	if err != nil {
		closeNodeApi()
		return nil, errors.As(err)
	}
	if v.APIVersion != build.APIVersion {
		closeNodeApi()
		return nil, xerrors.Errorf("lotus-storage-miner API version doesn't match: local: ", api.Version{APIVersion: build.APIVersion})
	}

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

	log.Info("Starting lotus worker")

	local := []*cli.Command{
		runCmd,
	}

	app := &cli.App{
		Name:    "lotus-seal-worker",
		Usage:   "Remote storage miner worker",
		Version: build.UserVersion(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"WORKER_PATH"},
				Value:   "~/.lotusstorage", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "storagerepo",
				EnvVars: []string{"LOTUS_STORAGE_PATH"},
				Value:   "~/.lotusstorage", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "sealedrepo",
				EnvVars: []string{"WORKER_SEALED_PATH"},
				Value:   "~/.lotusstorage", // TODO: Consider XDG_DATA_HOME
			},

			&cli.BoolFlag{
				Name:  "enable-gpu-proving",
				Usage: "enable use of GPU for mining operations",
				Value: true,
			},
			&cli.UintFlag{
				Name:  "cache-sectors",
				Value: 1,
			},
			&cli.BoolFlag{
				Name: "no-addpiece",
			},
			&cli.BoolFlag{
				Name: "no-precommit1",
			},
			&cli.BoolFlag{
				Name: "no-precommit2",
			},
			&cli.BoolFlag{
				Name: "no-commit1",
			},
			&cli.BoolFlag{
				Name: "no-commit2",
			},
			&cli.BoolFlag{
				Name:  "no-wpost",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "no-post",
				Value: true,
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
	Action: func(cctx *cli.Context) error {
		if !cctx.Bool("enable-gpu-proving") {
			os.Setenv("BELLMAN_NO_GPU", "true")
		}
		nodeCCtx = cctx

		nodeApi, err := GetNodeApi()
		if err != nil {
			return xerrors.Errorf("getting miner api: %w", err)
		}
		defer ReleaseNodeApi(true)

		ainfo, err := lcli.GetAPIInfo(cctx, repo.StorageMiner)
		if err != nil {
			return xerrors.Errorf("could not get api info: %w", err)
		}
		_, storageAddr, err := manet.DialArgs(ainfo.Addr)
		if err != nil {
			return err
		}
		fmt.Println(storageAddr)

		r, err := homedir.Expand(cctx.String("repo"))
		if err != nil {
			return err
		}

		sealedRepo, err := homedir.Expand(cctx.String("sealedrepo"))
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(nodeCCtx)
		act, err := nodeApi.ActorAddress(ctx)
		if err != nil {
			return err
		}
		ssize, err := nodeApi.ActorSectorSize(ctx, act)
		if err != nil {
			return err
		}

		workerAddr, err := nodeApi.WorkerAddress(ctx, act, types.EmptyTSK)
		if err != nil {
			return errors.As(err)
		}

		// fetch parameters from hlm
		// TODO: need more develop time
		//if err := FetchHlmParams(ctx, nodeApi, storageAddr); err != nil {
		//	return errors.As(err)
		//}

		if err := paramfetch.GetParams(ctx, build.ParametersJSON(), uint64(ssize)); err != nil {
			return xerrors.Errorf("get params: %w", err)
		}

		if err := os.MkdirAll(r, 0755); err != nil {
			return errors.As(err, r)
		}
		spt, err := ffiwrapper.SealProofTypeFromSectorSize(ssize)
		if err != nil {
			return xerrors.Errorf("getting proof type: %w", err)
		}
		cfg := &ffiwrapper.Config{
			SealProofType: spt,
		}

		sb, err := ffiwrapper.New(false, &basicfs.Provider{
			Root: r,
		}, cfg)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(sealedRepo, 0755); err != nil {
			return errors.As(err, sealedRepo)
		}
		sealedSB, err := ffiwrapper.New(false, &basicfs.Provider{
			Root: sealedRepo,
		}, cfg)
		if err != nil {
			return errors.As(err, sealedRepo)
		}
		// make download server
		fileServer := cctx.String("file-server")
		if len(fileServer) == 0 {
			netIp := os.Getenv("NETIP")
			fileServer = netIp + ":1280"
		}
		mux := mux.NewRouter()
		//storagerepo,err := homedir.Expand(cctx.String("storagerepo"))
		//if err != nil {
		//	return err
		//}
		log.Info("repo:", r)
		mux.PathPrefix("/file").HandlerFunc((&fileserver.FileHandle{Repo: r}).FileHttpServer)
		ah := &auth.Handler{
			Verify: AuthVerify,
			Next:   mux.ServeHTTP,
		}
		srv := &http.Server{Handler: ah}
		nl, err := net.Listen("tcp", fileServer)
		if err != nil {
			return err
		}

		//	fileServerToken, err := ioutil.ReadFile(filepath.Join(cctx.String("storagerepo"), "token"))
		//	if err != nil {
		//		return errors.As(err)
		//	}
		//	fileHandle := fileserver.NewStorageFileServer(r, string(fileServerToken), nil)
		go func() {
			//		log.Info("File server listen at: " + fileServer)
			//	if err := http.ListenAndServe(fileServer, fileHandle); err != nil {
			//		panic(err)
			//	}
			if err := srv.Serve(nl); err != nil {
				log.Warn(err)
			}

		}()

		for {

			select {
			case <-ctx.Done():
				break
			default:
				if err := acceptJobs(ctx,
					sb, sealedSB,
					act, workerAddr,
					"http://"+storageAddr, ainfo.AuthHeader(),
					"http://"+fileServer,
					r, sealedRepo,
					cctx.Uint("cache-sectors"),
					cctx.Bool("no-addpiece"), cctx.Bool("no-precommit1"), cctx.Bool("no-precommit2"), cctx.Bool("no-commit1"), cctx.Bool("no-commit2"),
					cctx.Bool("no-wpost"), cctx.Bool("no-post"),
				); err == nil {
					break
				} else {
					ReleaseNodeApi(false)
					log.Warn(err)
				}
				time.Sleep(3 * 1e9) // wait 3 seconds to reconnect.
			}
		}

		return nil
	},
}
