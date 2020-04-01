package main

import (
	"fmt"
	"strconv"

	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
	"gopkg.in/urfave/cli.v2"
)

var hlmWorkerCmd = &cli.Command{
	Name:  "hlm-worker",
	Usage: "Manage worker",
	Subcommands: []*cli.Command{
		infoHLMWorkerCmd,
		disableHLMWorkerCmd,
	},
}
var disableHLMWorkerCmd = &cli.Command{
	Name:      "disable",
	Usage:     "Disable a work node to stop allocating OR start allocating",
	ArgsUsage: "id and disable value",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() < 2 {
			return errors.New("need input workid AND disable value")
		}
		workerId := args.First()
		disable, err := strconv.ParseBool(args.Get(1))
		if err != nil {
			return errors.New("need input disable true/false")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.WorkerDisable(ctx, workerId, disable)
	},
}

var infoHLMWorkerCmd = &cli.Command{
	Name:  "list",
	Usage: "list worker status",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		infos, err := nodeApi.WorkerStatusAll(ctx)
		if err != nil {
			return errors.As(err)
		}
		for _, info := range infos {
			fmt.Println(info.String())
		}
		return nil
	},
}
