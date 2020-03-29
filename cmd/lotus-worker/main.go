package main

import (
	"os"
	"sync"
	"time"

	paramfetch "github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/sector-storage/ffiwrapper/basicfs"
	"github.com/mitchellh/go-homedir"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
	"gopkg.in/urfave/cli.v2"

	manet "github.com/multiformats/go-multiaddr-net"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/jsonrpc"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/filecoin-project/lotus/node/repo"

	"github.com/gwaylib/errors"
)

var log = logging.Logger("main")

const (
	workers   = 1 // TODO: Configurability
	transfers = 1
)

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
	time.Sleep(3e9)
}

func ReleaseNodeApi(shutdown bool) {
	nodeSync.Lock()
	defer nodeSync.Unlock()

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

	ctx := lcli.ReqContext(nodeCCtx)
	if nodeCCtx == nil {
		panic("need init node cctx")
	}

	if nodeApi != nil {
		return nodeApi, nil
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

func main() {
	lotuslog.SetupLogLevels()

	log.Info("Starting lotus worker")

	local := []*cli.Command{
		runCmd,
	}

	app := &cli.App{
		Name:    "lotus-seal-worker",
		Usage:   "Remote storage miner worker",
		Version: build.UserVersion,
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
			&cli.BoolFlag{
				Name: "no-addpiece",
			},
			&cli.BoolFlag{
				Name: "no-precommit",
			},
			&cli.BoolFlag{
				Name: "no-commit",
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

		if err := paramfetch.GetParams(build.ParametersJson(), uint64(ssize)); err != nil {
			return xerrors.Errorf("get params: %w", err)
		}

		if err := os.MkdirAll(r, 0755); err != nil {
			return errors.As(err, r)
		}
		_, spt, err := ffiwrapper.ProofTypeFromSectorSize(ssize)
		if err != nil {
			return xerrors.Errorf("getting proof type: %w", err)
		}
		cfg := &ffiwrapper.Config{
			RemoteMode:    true,
			SealProofType: spt,
		}

		sb, err := ffiwrapper.New(&basicfs.Provider{
			Root: r,
		}, cfg)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(sealedRepo, 0755); err != nil {
			return errors.As(err, sealedRepo)
		}
		sealedSB, err := ffiwrapper.New(&basicfs.Provider{
			Root: r,
		}, cfg)
		if err != nil {
			return errors.As(err, sealedRepo)
		}
		for {

			select {
			case <-ctx.Done():
				break
			default:
				if err := acceptJobs(ctx,
					sb, sealedSB,
					act, workerAddr,
					"http://"+storageAddr, ainfo.AuthHeader(),
					r, sealedRepo,
					cctx.Bool("no-addpiece"), cctx.Bool("no-precommit"), cctx.Bool("no-commit")); err == nil {
					break
				} else {
					log.Warn(err)
					ReleaseNodeApi(false)
				}
				time.Sleep(3 * 1e9) // wait 3 seconds to reconnect.
			}
		}
		return nil
	},
}
