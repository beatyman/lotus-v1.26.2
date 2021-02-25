package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
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
		proxyAutoCmd,
		proxyChangeCmd,
		proxyStatusCmd,
		proxyReloadCmd,
	},
}
var proxyAutoCmd = &cli.Command{
	Name:  "auto",
	Usage: "change the proxy auto select a node",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		on := false
		switch cctx.Args().First() {
		case "off":
			on = false
		case "on":
			on = true
		default:
			fmt.Println("need input 'on' or 'off'")
			return nil
		}

		ctx := lcli.ReqContext(cctx)
		return mApi.ProxyAutoSelect(ctx, on)
	},
}
var proxyChangeCmd = &cli.Command{
	Name:  "change",
	Usage: "change the proxy to a node of given",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		idx, err := strconv.Atoi(cctx.Args().First())
		if err != nil {
			fmt.Println("need index of list nodes")
			return nil
		}

		ctx := lcli.ReqContext(cctx)
		return mApi.ProxyChange(ctx, idx)
	},
}
var proxyStatusCmd = &cli.Command{
	Name:  "status",
	Usage: "get the proxy status",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "sync",
			Usage: "show the lotus sync status",
		},
		&cli.BoolFlag{
			Name:  "mpool",
			Usage: "show the local mpool",
		},
	},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)
		status, err := mApi.ProxyStatus(ctx, cctx.Bool("sync"), cctx.Bool("mpool"))
		if err != nil {
			return errors.As(err)
		}
		fmt.Printf("proxy on: %t, auto select: %t \n", status.ProxyOn, status.AutoSelect)
		fmt.Println("-----------------------------------")

		nodes := status.Nodes
		fmt.Println("lotus nodes:")
		for i := 0; i < len(nodes); i++ {
			fmt.Printf(
				"idx:%d, addr:%s, using:%t, alive:%t, height:%d, used-times:%d\n",
				i, nodes[i].Addr, nodes[i].Using, nodes[i].Alive, nodes[i].Height, nodes[i].UsedTimes,
			)
		}
		if cctx.Bool("mpool") {
			fmt.Println()
			fmt.Println("lotus mpool stat:")

			for i := 0; i < len(nodes); i++ {
				fmt.Printf("addr:%s, \n", nodes[i].Addr)
				var total api.ProxyMpStat
				total.GasLimit = big.Zero()
				for _, stat := range nodes[i].MpoolStat {
					total.Past += stat.Past
					total.Cur += stat.Cur
					total.Future += stat.Future
					total.GasLimit = big.Add(total.GasLimit, stat.GasLimit)

					fmt.Printf(
						"%s: Nonce past: %d, cur: %d, future: %d; gasLimit: %s\n",
						stat.Addr, stat.Past, stat.Cur, stat.Future, stat.GasLimit,
					)
				}

				fmt.Println("-----")
				fmt.Printf(
					"total: Nonce past: %d, cur: %d, future: %d; gasLimit: %s\n",
					total.Past, total.Cur, total.Future, total.GasLimit,
				)
			}
		}
		if cctx.Bool("sync") {
			fmt.Println()
			fmt.Println("lotus sync:")
			for i := 0; i < len(nodes); i++ {
				fmt.Printf("addr:%s, ", nodes[i].Addr)
				// for sync status
				state := nodes[i].SyncStat
				if state == nil {
					fmt.Println("no stats")
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
