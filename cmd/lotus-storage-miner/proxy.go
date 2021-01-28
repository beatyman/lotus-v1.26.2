package main

import (
	"fmt"

	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/urfave/cli/v2"
)

var proxyCmd = &cli.Command{
	Name:  "proxy",
	Usage: "Manage proxy",
	Subcommands: []*cli.Command{
		proxyStatusCmd,
		proxyReloadCmd,
	},
}
var proxyStatusCmd = &cli.Command{
	Name:  "status",
	Usage: "get the proxy status",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)
		status, err := mApi.ProxyStatus(ctx)
		if err != nil {
			return err
		}
		fmt.Println("in using node:")
		fmt.Println("------------------")
		fmt.Printf("addr:%s, alive: %t, height:%d, used-times:%d\n", status[0].Addr, status[0].Alive, status[0].Height, status[0].UsedTimes)
		fmt.Println()
		fmt.Println("all lotus node:")
		fmt.Println("------------------")
		for i := 1; i < len(status); i++ {
			fmt.Printf("addr:%s, alive: %t, height:%d, used-times:%d\n", status[i].Addr, status[i].Alive, status[i].Height, status[i].UsedTimes)
		}
		return nil
	},
}
var proxyReloadCmd = &cli.Command{
	Name:  "reload",
	Usage: "Reload the proxy configration file",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)
		return mApi.ProxyReload(ctx)
	},
}
