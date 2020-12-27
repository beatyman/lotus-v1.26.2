package main

import (
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

//PartitionNumber
var wdpostCmd = &cli.Command{
	Name:  "wdpost",
	Usage: "tune wdpost params",
	Subcommands: []*cli.Command{
		wdpostEnableSeparatePartitionCmd,
		wdpostSetPartitionNumberCmd,
	},
}

var wdpostEnableSeparatePartitionCmd = &cli.Command{
	Name:  "EnableSeparatePartition",
	Usage: "Enable Separate Partition",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "enable",
			Usage: "enable",
		},
	},
	//SetPartitionNumber
	Action: func(cctx *cli.Context) error {
		nodeAPI, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting full node api: %w", err)
		}
		defer closer()
		enable := cctx.Bool("enable")
		ctx := lcli.DaemonContext(cctx)
		return nodeAPI.WdpostEnablePartitionSeparate(ctx, enable)
	},
}
var wdpostSetPartitionNumberCmd = &cli.Command{
	Name:  "SetPartitionNumber",
	Usage: "set partition number per message",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "PartitionNumber",
			Usage: "partition number per message",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeAPI, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting full node api: %w", err)
		}
		defer closer()
		ctx := lcli.DaemonContext(cctx)
		partitionNumber := cctx.Int("PartitionNumber")
		return nodeAPI.WdpostSetPartitionNumber(ctx, partitionNumber)
	},
}
