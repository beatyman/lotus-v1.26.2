package main

import (
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/gocarina/gocsv"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

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
		rebuildPledgeSectorCmd,
	},
}

var hlmSectorCmd = &cli.Command{
	Name:  "hlm-sector",
	Usage: "command for hlm-sector",
	Subcommands: []*cli.Command{
		getHlmSectorStateCmd,
		setHlmSectorStateCmd,
		checkHlmSectorCmd,
		setHlmSectorStartCmd,
		getHlmSectorStartCmd,
		sectorsExportExpireCmd,
	},
}

var startPledgeSectorCmd = &cli.Command{
	Name:  "start",
	Usage: "start the pledge daemon",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return mApi.RunPledgeSector(ctx)
	},
}

var statusPledgeSectorCmd = &cli.Command{
	Name:  "status",
	Usage: "the pledge daemon status",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if status, err := mApi.StatusPledgeSector(ctx); err != nil {
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
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return mApi.StopPledgeSector(ctx)
	},
}
var rebuildPledgeSectorCmd = &cli.Command{
	Name:  "rebuild",
	Usage: "will rebuild the garbage sector",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "sector-id",
			Usage: "sector id which want to set",
		},
		&cli.Uint64Flag{
			Name:  "storage-id",
			Usage: "storage id which want to replaced",
		},
	},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		sid := cctx.String("sector-id")
		if len(sid) == 0 {
			return errors.New("need input --sector-id=s-t0xxxx-xxx")
		}
		storage := cctx.Uint64("storage-id")
		if storage == 0 {
			return errors.New("need input --storage-id=x")
		}
		return mApi.RebuildPledgeSector(ctx, sid, storage)
	},
}

var getHlmSectorStateCmd = &cli.Command{
	Name:  "get",
	Usage: "get the sector info by sector id(s-t0xxx-x)",
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		sid := cctx.Args().First()
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		info, err := mApi.HlmSectorGetState(ctx, sid)
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
var setHlmSectorStateCmd = &cli.Command{
	Name:  "set-state",
	Usage: "will set the sector state",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "sector-id",
			Usage: "sector id which want to set",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "force to release the working task",
		},
		&cli.BoolFlag{
			Name:  "reset",
			Usage: "reset the state, or it will be added, default is added",
			Value: false,
		},
		&cli.IntFlag{
			Name:  "state",
			Usage: "state which want to set",
		},
		&cli.StringFlag{
			Name:  "memo",
			Usage: "memo for state udpate",
		},
	},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		sid := cctx.String("sector-id")
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		memo := cctx.String("memo")
		if len(memo) == 0 {
			return errors.New("need input memo")
		}
		if _, err := mApi.HlmSectorSetState(ctx, sid, memo, cctx.Int("state"), cctx.Bool("force"), cctx.Bool("reset")); err != nil {
			return err
		}
		info, err := mApi.HlmSectorGetState(ctx, sid)
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
var checkHlmSectorCmd = &cli.Command{
	Name:  "check",
	Usage: "checking provable of the sector",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:  "timeout",
			Usage: "the unit is second",
			Value: 6,
		},
	},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		sid := cctx.Args().First()
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		used, err := mApi.HlmSectorCheck(ctx, sid, time.Duration(cctx.Int64("timeout"))*time.Second)
		if err != nil {
			return err
		}
		fmt.Printf("used:%s\n", used)
		return nil
	},
}
var setHlmSectorStartCmd = &cli.Command{
	Name:  "set-start-id",
	Usage: "set the sector id start",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		startIDStr := cctx.Args().First()
		startID, err := strconv.ParseUint(startIDStr, 10, 64)
		if err != nil {
			return errors.As(err, startIDStr)
		}
		if err := mApi.HlmSectorSetStartID(ctx, startID); err != nil {
			return err
		}
		return nil
	},
}
var getHlmSectorStartCmd = &cli.Command{
	Name:  "get-start-id",
	Usage: "get the sector id start",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		startID, err := mApi.HlmSectorGetStartID(ctx)
		if err != nil {
			return err
		}
		fmt.Println("start id:", startID)
		return nil
	},
}

var getHlmSectorByWorker = &cli.Command{
	Name:  "get-sector-worker",
	Usage: "get the sector by worker",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		mApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		sid := cctx.Args().First()

		if len(sid) == 0 {
			return errors.New("sid is null")
		}

		fmt.Println("sid ====", sid)

		ctx := lcli.ReqContext(cctx)
		//查询workerlist
		workers, err := mApi.WorkerStatusAll(ctx)
		if err != nil {
			return err
		}

		sectorInfo, err := mApi.HlmSectorGetState(ctx, sid)
		workerIp := ""
		for _, val := range workers {
			if val.ID == sectorInfo.WorkerId {
				workerIp = val.IP
			}
		}
		fmt.Println("sector state: ", sectorInfo.State, "  workerIP: ", workerIp)
		//

		//查询密封表
		now := time.Now()
		lastMonth := now.AddDate(0, -1, -now.Day()+1).Format("200601")
		currentMonth := now.Format("200601")
		sectorSeal, err := mApi.HlmSectorByWorker(ctx, sid, lastMonth, currentMonth)
		for _, val := range sectorSeal {
			ip := ""
			for _, worker := range workers {
				if val.WorkerID == worker.ID {
					ip = worker.IP
				}
			}
			fmt.Println("state: : ", val.Stage, " workerIP: ", ip)
		}

		return nil
	},
}

type ExportSectors struct {
	ID              uint64 `csv:"id"`
	SealProof       int64  `csv:"sealProof"`
	InitialPledge   string `csv:"initialPledge"`
	ActivationEpoch int64  `csv:"activation_epoch"`
	ExpirationEpoch int64  `csv:"expiration_epoch"`
	Activation      string `csv:"activation"`
	Expiration      string `csv:"expiration"`
	MaxExpiration   string `csv:"maxExpiration"`
	MaxExtendNow    string `csv:"maxExtendNow"`
	DealIDs         string `csv:"dealIDs"`
	Deadline        uint64 `csv:"deadline"`
	Partition       uint64 `csv:"partition"`
	DC              bool   `csv:"dc"`
}

func DealIDsToString(deals []abi.DealID) string {
	dls := make([]string, 0)
	for i := range deals {
		dls = append(dls, strconv.Itoa(int(deals[i])))
	}
	return strings.Join(dls, " ")
}

var sectorsExportExpireCmd = &cli.Command{
	Name:  "export-expire",
	Usage: "export expiring sectors",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:  "cutoff",
			Usage: "skip sectors whose current expiration is more than <cutoff> epochs from now, defaults to 60 days",
			Value: 172800,
		},
		&cli.StringFlag{
			Name:  "output",
			Usage: "output path",
			Value: "/root/export-expire.csv",
		},
	},
	Action: func(cctx *cli.Context) error {

		fullApi, nCloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer nCloser()

		ctx := lcli.ReqContext(cctx)

		maddr, err := getActorAddress(ctx, cctx)
		if err != nil {
			return err
		}

		head, err := fullApi.ChainHead(ctx)
		if err != nil {
			return err
		}
		currEpoch := head.Height()

		nv, err := fullApi.StateNetworkVersion(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}

		sectors, err := fullApi.StateMinerActiveSectors(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		n := 0
		for _, s := range sectors {
			if s.Expiration-currEpoch <= abi.ChainEpoch(cctx.Int64("cutoff")) {
				sectors[n] = s
				n++
			}
		}
		sectors = sectors[:n]

		sort.Slice(sectors, func(i, j int) bool {
			if sectors[i].Expiration == sectors[j].Expiration {
				return sectors[i].SectorNumber < sectors[j].SectorNumber
			}
			return sectors[i].Expiration < sectors[j].Expiration
		})
		exportList := make([]*ExportSectors, 0)
		for _, sector := range sectors {
			MaxExpiration := sector.Activation + policy.GetSectorMaxLifetime(sector.SealProof, nv)
			maxExtension, err := policy.GetMaxSectorExpirationExtension(nv)
			if err != nil {
				return xerrors.Errorf("failed to get max extension: %w", err)
			}
			MaxExtendNow := currEpoch + maxExtension
			if MaxExtendNow > MaxExpiration {
				MaxExtendNow = MaxExpiration
			}
			si, err := fullApi.StateSectorGetInfo(ctx, maddr, sector.SectorNumber, types.EmptyTSK)
			if err != nil {
				return err
			}
			sp, err := fullApi.StateSectorPartition(ctx, maddr, sector.SectorNumber, types.EmptyTSK)
			if err != nil {
				return err
			}
			exportList = append(exportList, &ExportSectors{
				ID:              uint64(sector.SectorNumber),
				SealProof:       int64(sector.SealProof),
				InitialPledge:   types.FIL(sector.InitialPledge).Short(),
				ActivationEpoch: int64(sector.Activation),
				ExpirationEpoch: int64(sector.Expiration),
				Activation:      cliutil.EpochTime(currEpoch, sector.Activation),
				Expiration:      cliutil.EpochTime(currEpoch, sector.Expiration),
				MaxExpiration:   cliutil.EpochTime(currEpoch, MaxExpiration),
				MaxExtendNow:    cliutil.EpochTime(currEpoch, MaxExtendNow),
				DealIDs:         DealIDsToString(si.DealIDs),
				Deadline:        sp.Deadline,
				Partition:       sp.Partition,
				DC:              len(si.DealIDs) > 0,
			})
		}
		file, err := os.Create(cctx.String("output"))
		if err != nil {
			return err
		}
		defer file.Close()
		if err := gocsv.MarshalFile(&exportList, file); err != nil {
			return err
		}
		return nil
	}}
