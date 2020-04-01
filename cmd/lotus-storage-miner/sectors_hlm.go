package main

import (
	"fmt"

	"gopkg.in/urfave/cli.v2"

	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
)

var pledgeSectorCmd = &cli.Command{
	Name:  "pledge-sector",
	Usage: "Pledge sector",
	Subcommands: []*cli.Command{
		startPledgeSectorCmd,
		statusPledgeSectorCmd,
		stopPledgeSectorCmd,
	},
}

var startPledgeSectorCmd = &cli.Command{
	Name:  "start",
	Usage: "start the pledge daemon",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return nodeApi.RunPledgeSector(ctx)
	},
}

var statusPledgeSectorCmd = &cli.Command{
	Name:  "status",
	Usage: "the pledge daemon status",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if status, err := nodeApi.StatusPledgeSector(ctx); err != nil {
			return errors.As(err)
		} else if status != 0 {
			fmt.Println("Running")
		} else {
			fmt.Println("Not Running")
		}
		return nil
	},
}
var stopPledgeSectorCmd = &cli.Command{
	Name:  "stop",
	Usage: "stop the pledge daemon",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return nodeApi.StopPledgeSector(ctx)
	},
}
