// +build !nodaemon

package main

import (
	"context"
	"io/ioutil"

	"github.com/multiformats/go-multiaddr"
	"golang.org/x/xerrors"
	"gopkg.in/urfave/cli.v2"

	"github.com/filecoin-project/go-lotus/api"
	"github.com/filecoin-project/go-lotus/build"
	"github.com/filecoin-project/go-lotus/node"
	"github.com/filecoin-project/go-lotus/node/modules"
	"github.com/filecoin-project/go-lotus/node/modules/testing"
	"github.com/filecoin-project/go-lotus/node/repo"
)

const (
	makeGenFlag = "lotus-make-random-genesis"
)

// DaemonCmd is the `go-lotus daemon` command
var DaemonCmd = &cli.Command{
	Name:  "daemon",
	Usage: "Start a lotus daemon process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "api",
			Value: "1234",
		},
		&cli.StringFlag{
			Name:   makeGenFlag,
			Value:  "",
			Hidden: true,
		},
		&cli.StringFlag{
			Name:  "genesis",
			Usage: "genesis file to use for first node run",
		},
		&cli.BoolFlag{
			Name:  "bootstrap",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		r, err := repo.NewFS(cctx.String("repo"))
		if err != nil {
			return err
		}

		if err := r.Init(); err != nil && err != repo.ErrRepoExists {
			return err
		}

		if err := build.GetParams(false); err != nil {
			return xerrors.Errorf("fetching proof parameters: %w", err)
		}

		genBytes := build.MaybeGenesis()

		if cctx.String("genesis") != "" {
			genBytes, err = ioutil.ReadFile(cctx.String("genesis"))
			if err != nil {
				return err
			}

		}

		genesis := node.Options()
		if len(genBytes) > 0 {
			genesis = node.Override(new(modules.Genesis), modules.LoadGenesis(genBytes))
		}
		if cctx.String(makeGenFlag) != "" {
			genesis = node.Override(new(modules.Genesis), testing.MakeGenesis(cctx.String(makeGenFlag)))
		}

		var api api.FullNode
		stop, err := node.New(ctx,
			node.FullAPI(&api),

			node.Online(),
			node.Repo(r),

			genesis,

			node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
				apima, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/" + cctx.String("api"))
				if err != nil {
					return err
				}
				return lr.SetAPIEndpoint(apima)
			}),
		)
		if err != nil {
			return err
		}

		go func() {
			if !cctx.Bool("bootstrap") {
				return
			}
			err := bootstrap(ctx, api)
			if err != nil {
				log.Error("Bootstrap failed: ", err)
			}
		}()

		// TODO: properly parse api endpoint (or make it a URL)
		return serveRPC(api, stop, "127.0.0.1:"+cctx.String("api"))
	},
}

func bootstrap(ctx context.Context, api api.FullNode) error {
	pis, err := build.BuiltinBootstrap()
	if err != nil {
		return err
	}

	for _, pi := range pis {
		if err := api.NetConnect(ctx, pi); err != nil {
			return err
		}
	}

	return nil
}
