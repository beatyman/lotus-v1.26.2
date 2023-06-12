package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/monitor"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace/spans"
	"sort"
	"time"

	"github.com/filecoin-project/lotus/api"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"
)

var hlmWorkerCmd = &cli.Command{
	Name:  "hlm-worker",
	Usage: "Manage worker",
	Subcommands: []*cli.Command{
		statusHLMWorkerCmd,
		listHLMWorkerCmd,
		getHLMWorkerCmd,
		searchHLMWorkerCmd,
		gcHLMWorkerCmd,
		enableHLMWorkerCmd,
		disableHLMWorkerCmd,
		pauseHLMSealCmd,
		unpauseHLMSealCmd,
		producerIdleCmd,
		statSealNumCmd,
		statSealTimeCmd,
	},
}

func printWorkerStat(ctx context.Context, nodeApi api.StorageMiner) error {
	wstat, err := nodeApi.WorkerStatus(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Worker use:\n")
	fmt.Printf("\tPauseSeal : %t \n", wstat.PauseSeal != 0)
	fmt.Printf("\tSealWorker: %d / %d (locked: %d)\n", wstat.SealWorkerUsing, wstat.SealWorkerTotal, wstat.SealWorkerLocked)
	fmt.Printf("\tCommit2Srv: %d / %d\n", wstat.Commit2SrvUsed, wstat.Commit2SrvTotal)
	fmt.Printf("\tWnPoStSrv : %d / %d\n", wstat.WnPoStSrvUsed, wstat.WnPoStSrvTotal)
	fmt.Printf("\tWdPoStSrv : %d / %d\n", wstat.WdPoStSrvUsed, wstat.WdPoStSrvTotal)
	fmt.Printf("\tAllRemotes: all:%d, online:%d, offline:%d, disabled: %d\n", wstat.WorkerOnlines+wstat.WorkerOfflines+wstat.WorkerDisabled, wstat.WorkerOnlines, wstat.WorkerOfflines, wstat.WorkerDisabled)

	fmt.Printf("Queues:\n")
	fmt.Printf("\tAddPiece: %d\n", wstat.PledgeWait)
	fmt.Printf("\tPreCommit1: %d\n", wstat.PreCommit1Wait)
	fmt.Printf("\tPreCommit2: %d\n", wstat.PreCommit2Wait)
	fmt.Printf("\tCommit: %d\n", wstat.CommitWait)
	fmt.Printf("\tFinalize: %d\n", wstat.FinalizeWait)
	fmt.Printf("\tUnseal: %d\n", wstat.UnsealWait)
	return nil
}

var statusHLMWorkerCmd = &cli.Command{
	Name:    "status",
	Aliases: []string{"info"},
	Usage:   "workers status",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return printWorkerStat(ctx, nodeApi)
	},
}
var listHLMWorkerCmd = &cli.Command{
	Name:  "list",
	Usage: "list worker status",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "online",
			Usage: "show the online worker",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "offline",
			Usage: "show the offline worker",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "disabled",
			Usage: "show the disabled worker",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "service",
			Usage: "show the service worker",
			Value: true,
		},
		&cli.IntFlag{
			Name:  "overdue",
			Usage: "show the overdue tasks, unit in hour",
			Value: 24,
		},
	},
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
		showDisabled := cctx.Bool("disabled")
		showOnline := cctx.Bool("online")
		showOffline := cctx.Bool("offline")
		showService := cctx.Bool("service")
		overdueTask := cctx.Int("overdue")

		if showDisabled {
			fmt.Println("============== Disabled worker ===============")
			for _, info := range infos {
				if !info.Disable {
					continue
				}
				fmt.Println(info.String())
			}
			fmt.Println("============== Disabled worker end ===============")
		}

		if showOnline {
			fmt.Println("============== Online worker ===============")
			for _, info := range infos {
				if info.Online && !info.Disable {
					fmt.Println(info.String())
				}
			}
			fmt.Println("============== Online worker end ===============")
		}

		if showService {
			fmt.Println("============== Service worker ===============")
			for _, info := range infos {
				if info.Srv && !info.Disable {
					fmt.Println(info.String())
				}
			}
			fmt.Println("============== Service worker end ===============")
		}

		if showOffline {
			fmt.Println("============== Offline worker ===============")
			for _, info := range infos {
				if !info.Online && !info.Disable {
					fmt.Println(info.String())
				}
			}
			fmt.Println("============== Offline worker end ===============")
		}
		if overdueTask > 0 {
			fmt.Println("============== Overdue tasks ===============")
			now := time.Now()
			for _, info := range infos {
				for _, sInfo := range info.SectorOn {
					sub := now.Sub(sInfo.CreateTime)
					if sub < (time.Duration(overdueTask) * time.Hour) {
						continue
					}
					fmt.Printf("created_at:%s, sector:%s, state:%d, worker:%s, overdue:%s\n", sInfo.CreateTime.Format(time.RFC3339), sInfo.ID, sInfo.State, info.ID, sub.String())
				}
			}
			fmt.Println("============== Overdue tasks end ===============")
		}

		return nil
	},
}
var getHLMWorkerCmd = &cli.Command{
	Name:      "get",
	Usage:     "get worker detail",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		info, err := nodeApi.WorkerInfo(ctx, workerId)
		if err != nil {
			return err
		}
		output, err := json.MarshalIndent(info, "", "	")
		if err != nil {
			return err
		}
		fmt.Println(string(output))
		return nil
	},
}
var searchHLMWorkerCmd = &cli.Command{
	Name:      "search",
	Usage:     "search worker with ip",
	ArgsUsage: "worker ip",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		infos, err := nodeApi.WorkerSearch(ctx, workerId)
		if err != nil {
			return err
		}
		output, err := json.MarshalIndent(infos, "", "	")
		if err != nil {
			return err
		}
		fmt.Println(string(output))
		return nil
	},
}
var gcHLMWorkerCmd = &cli.Command{
	Name:      "gc",
	Usage:     "gc worker who state is more than 200",
	ArgsUsage: "workid/all",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid/all")
		}
		if workerId == "all" {
			workerId = ""
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		gcTasks, err := nodeApi.WorkerGcLock(ctx, workerId)
		if err != nil {
			return err
		}
		for _, task := range gcTasks {
			fmt.Printf("gc : %s\n", task)
		}
		fmt.Println("gc done")
		return nil
	},
}
var enableHLMWorkerCmd = &cli.Command{
	Name:      "enable",
	Usage:     "Enable a work node to start allocating",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input worker id")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		addr, _ := nodeApi.ActorAddress(ctx)
		//添加监控
		kind := model.PackageKind_Miner

		monitor.Init(kind, addr.String())
		_, span := spans.NewWorkerOperationSpan(context.Background())
		span.SetWorkNo(workerId)
		span.SetState(false)
		span.End()

		return nodeApi.WorkerDisable(ctx, workerId, false)
	},
}
var disableHLMWorkerCmd = &cli.Command{
	Name:      "disable",
	Usage:     "Disable a work node to stop allocating OR start allocating",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input worker id")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		addr, _ := nodeApi.ActorAddress(ctx)
		//添加监控
		kind := model.PackageKind_Miner

		monitor.Init(kind, addr.String())
		_, span := spans.NewWorkerOperationSpan(context.Background())
		span.SetWorkNo(workerId)
		span.SetState(true)
		span.End()
		return nodeApi.WorkerDisable(ctx, workerId, true)
	},
}
var pauseHLMSealCmd = &cli.Command{
	Name:  "pause",
	Usage: "pause seal for all worker, so it can control the base fee.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "really-do-it",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if !cctx.Bool("really-do-it") {
			fmt.Println("need input really-do-it for confirm")
			return nil
		}
		ctx := lcli.ReqContext(cctx)
		if err := nodeApi.PauseSeal(ctx, 1); err != nil {
			return err
		}
		return printWorkerStat(ctx, nodeApi)
	},
}
var unpauseHLMSealCmd = &cli.Command{
	Name:  "unpause",
	Usage: "unpause seal for all worker, so it can control the base fee.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "really-do-it",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if !cctx.Bool("really-do-it") {
			fmt.Println("need input really-do-it for confirm")
			return nil
		}
		ctx := lcli.ReqContext(cctx)
		if err := nodeApi.PauseSeal(ctx, 0); err != nil {
			return err
		}
		return printWorkerStat(ctx, nodeApi)
	},
}

var producerIdleCmd = &cli.Command{
	Name:  "producer-idle",
	Usage: "stat idle of the worker producer",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		num, err := nodeApi.WorkerProducerIdle(ctx)
		if err != nil {
			return errors.As(err)
		}
		fmt.Println(num)
		return nil
	},
}
var statSealNumCmd = &cli.Command{
	Name:  "stat-seal-num",
	Usage: "statis worker seal count number",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "stat-time",
			Usage: "format is '2006-01-02 15:04:05' in local zone, using current time is not set",
		},
		&cli.IntFlag{
			Name:  "past-hours",
			Value: 12,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		endTime := time.Now()
		endTimeStr := cctx.String("stat-time")
		if len(endTimeStr) > 0 {
			stTime, err := time.ParseInLocation("2006-01-02 15:04:05", endTimeStr, time.Local)
			if err != nil {
				return errors.As(err, endTimeStr)
			}
			endTime = stTime
		}

		pastHours := cctx.Int("past-hours")
		if pastHours <= 0 {
			return errors.New("error past-hours")
		}
		startTime := endTime.Add(time.Duration(-pastHours) * time.Hour)
		result, err := nodeApi.StatWorkerSealNumFn(ctx, startTime, endTime)
		if err != nil {
			return err
		}
		fmt.Printf("begin-time:%s, end-time:%s\n",
			startTime.Format(time.RFC3339),
			endTime.Format(time.RFC3339),
		)
		result.SortByFinalized()
		result.Dump()
		result.SumDump()
		return nil
	},
}

var statSealTimeCmd = &cli.Command{
	Name:  "stat-seal-time",
	Usage: "statis worker seal time",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "worker-id",
			Usage: "Show all workers if id not set",
		},
		&cli.StringFlag{
			Name:  "stat-time",
			Usage: "format is '2006-01-06 15:04:05' in local zone, using current time is not set",
		},
		&cli.IntFlag{
			Name:  "past-hours",
			Value: 24,
		},
		&cli.BoolFlag{
			Name:  "show-disabled",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "show-commit2",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "show-wdpost",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "show-wnpost",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		endTime := time.Now()
		endTimeStr := cctx.String("stat-time")
		if len(endTimeStr) > 0 {
			stTime, err := time.ParseInLocation("2006-01-02 15:04:05", endTimeStr, time.Local)
			if err != nil {
				return errors.As(err, endTimeStr)
			}
			endTime = stTime
		}

		pastHours := cctx.Int("past-hours")
		if pastHours <= 0 {
			return errors.New("error past-hours")
		}
		workerID := cctx.String("worker-id")
		startTime := endTime.Add(time.Duration(-pastHours) * time.Hour)
		fmt.Printf("begin-time:%s, end-time:%s\n",
			startTime.Format(time.RFC3339),
			endTime.Format(time.RFC3339),
		)
		if len(workerID) > 0 {
			result, err := nodeApi.StatWorkerSealTimeFn(ctx, workerID, startTime, endTime)
			if err != nil {
				return err
			}
			result.Dump()
		} else {
			// show all
			infos, err := nodeApi.WorkerStatusAll(ctx)
			if err != nil {
				return errors.As(err)
			}
			output := []struct {
				Worker ffiwrapper.WorkerRemoteStats
				Seal   database.StatWorkerSealTimes
			}{}
			showDisabled := cctx.Bool("show-disabled")
			showCommit2Srv := cctx.Bool("show-commit2")
			showWdPoStSrv := cctx.Bool("show-wdpost")
			showWnPoStSrv := cctx.Bool("show-wnpost")
			for _, r := range infos {
				if r.Disable && !showDisabled {
					continue
				}
				if r.Cfg.Commit2Srv && !showCommit2Srv {
					continue
				}
				if r.Cfg.WdPoStSrv && !showWdPoStSrv {
					continue
				}
				if r.Cfg.WnPoStSrv && !showWnPoStSrv {
					continue
				}
				result, err := nodeApi.StatWorkerSealTimeFn(ctx, r.ID, startTime, endTime)
				if err != nil {
					return err
				}
				if len(result) > 0 {
					output = append(output, struct {
						Worker ffiwrapper.WorkerRemoteStats
						Seal   database.StatWorkerSealTimes
					}{
						Worker: r,
						Seal:   result,
					})
				}
			}
			// sort
			sort.Slice(output, func(i, j int) bool {
				if output[i].Worker.Srv != output[j].Worker.Srv {
					return output[i].Worker.Srv && !output[j].Worker.Srv
				}
				if output[i].Worker.IP != output[j].Worker.IP {
					return output[i].Worker.IP < output[j].Worker.IP
				}
				return output[i].Worker.ID < output[j].Worker.ID
			})
			for _, o := range output {
				fmt.Println()
				fmt.Printf("worker:%s(%s),p1:%d,p2:%d,c2:%t,wdpost:%t,wnpost:%t\n",
					o.Worker.ID[:8], o.Worker.IP, o.Worker.Cfg.ParallelPrecommit1, o.Worker.Cfg.ParallelPrecommit2,
					o.Worker.Cfg.Commit2Srv, o.Worker.Cfg.WdPoStSrv, o.Worker.Cfg.WnPoStSrv,
				)
				o.Seal.Dump()
			}
		}
		return nil
	},
}
