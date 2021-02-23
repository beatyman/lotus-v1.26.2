package main

import (
	"fmt"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"

	"github.com/gwaylib/errors"
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
		// in using
		usingAddr, err := mApi.ProxyUsing(ctx)
		if err != nil {
			return errors.As(err)
		}

		status, err := mApi.ProxyStatus(ctx)
		if err != nil {
			return errors.As(err)
		}
		fmt.Println("all lotus node:")
		fmt.Println("------------------")
		for i := 0; i < len(status); i++ {
			fmt.Printf("addr:%s, inusing:%t, alive: %t, height:%d, used-times:%d\n", status[i].Addr == usingAddr, status[i].Addr, status[i].Alive, status[i].Height, status[i].UsedTimes)
		}

		fmt.Println()
		fmt.Println("all lotus sync:")
		fmt.Println("------------------")
		for i := 0; i < len(status); i++ {
			fmt.Printf("addr:%s, alive: %t, height:%d, used-times:%d\n", status[i].Addr, status[i].Alive, status[i].Height, status[i].UsedTimes)
			// for sync status
			state := status[i].SyncStat
			if state == nil {
				continue
			}
			result := []api.ActiveSync{}
			for i := len(state.ActiveSyncs) - 1; i > -1; i-- {
				result = append(result, state.ActiveSyncs[i])
			}
			fmt.Println("sync status:")
			for _, ss := range result {
				fmt.Printf("worker %d:\n", ss.WorkerID)
				var base, target []cid.Cid
				var heightDiff int64
				var theight abi.ChainEpoch
				if ss.Base != nil {
					base = ss.Base.Cids()
					heightDiff = int64(ss.Base.Height())
				}
				if ss.Target != nil {
					target = ss.Target.Cids()
					heightDiff = int64(ss.Target.Height()) - heightDiff
					theight = ss.Target.Height()
				} else {
					heightDiff = 0
				}
				fmt.Printf("\tBase:\t%s\n", base)
				fmt.Printf("\tTarget:\t%s (%d)\n", target, theight)
				fmt.Printf("\tHeight diff:\t%d\n", heightDiff)
				fmt.Printf("\tStage: %s\n", ss.Stage)
				fmt.Printf("\tHeight: %d\n", ss.Height)
				if ss.End.IsZero() {
					if !ss.Start.IsZero() {
						fmt.Printf("\tElapsed: %s\n", time.Since(ss.Start))
					}
				} else {
					fmt.Printf("\tElapsed: %s\n", ss.End.Sub(ss.Start))
				}
				if ss.Stage == api.StageSyncErrored {
					fmt.Printf("\tError: %s\n", ss.Message)
				}
			}
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
