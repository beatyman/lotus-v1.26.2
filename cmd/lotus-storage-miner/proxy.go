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
		if len(status) == 0 {
			fmt.Println("proxy is not running")
			return nil
		}
		for _, s := range status {
			fmt.Printf("addr:%s,alive:%t,height:%d,escape:%s\n", s.Addr, s.Alive, s.Height, s.Escape)
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
