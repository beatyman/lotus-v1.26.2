package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/unixfs"
	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil/cidenc"
	"github.com/multiformats/go-multibase"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var hlmMarketCmd = &cli.Command{
	Name:  "hlm-market",
	Usage: "Manage market",
	Subcommands: []*cli.Command{
		hlmDealCleanCmd,
		hlmDealMakeCmd,
		hlmDealPathCmd,
		hlmDealRecordCmd,
		hlmDealInfoCmd,
		hlmDealSectorCmd,
	},
}
var hlmDealCleanCmd = &cli.Command{
	Name:      "clean-deal",
	Usage:     "Manually remove the deal-staging files overdue from now",
	ArgsUsage: "<deal-staging-dir>",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "s-overdue",
			Usage: "overdue from now, check the seal task is it still busy. it will break the clean if it's not empty.",
			Value: 11,
		},
		&cli.IntFlag{
			Name:  "f-overdue",
			Usage: "overdue from now, default will clean the file of 12 hours ago",
			Value: 13,
		},
		&cli.StringFlag{
			Name:  "file-head",
			Usage: "treat the file which has the prefix",
			Value: "fstmp",
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "really remove the deal-staging data",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := lcli.DaemonContext(cctx)
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		dirPath := cctx.Args().First()
		if len(dirPath) == 0 {
			return errors.New("need input deal-staging-dir")
		}
		infos, err := nodeApi.WorkerStatusAll(ctx)
		if err != nil {
			return errors.As(err)
		}
		result := []string{}
		sOverdue := time.Duration(cctx.Int("s-overdue")) * time.Hour
		now := time.Now()
		for _, info := range infos {
			for _, sInfo := range info.SectorOn {
				sub := now.Sub(sInfo.CreateTime)
				if sub < sOverdue {
					continue
				}
				result = append(result, fmt.Sprintf("created_at:%s, sector:%s, state:%d, worker:%s, overdue:%s", sInfo.CreateTime.Format(time.RFC3339), sInfo.ID, sInfo.State, info.ID, sub.String()))
			}
		}
		if len(result) > 0 {
			fmt.Printf("============== Overdue tasks (%d) ===============\n", len(result))
			for _, info := range result {
				fmt.Println(info)
			}
			fmt.Println("============== Overdue tasks end ===============")
			return errors.New("overdue task is not empty")
		}

		files, err := ioutil.ReadDir(dirPath)
		if err != nil {
			return errors.As(err, dirPath)
		}
		fOverdue := time.Duration(cctx.Int("f-overdue")) * time.Hour
		prefix := cctx.String("file-head")
		reallyDoIt := cctx.Bool("really-do-it")
		for _, file := range files {
			sub := now.Sub(file.ModTime())
			if sub < fOverdue {
				continue
			}
			if len(prefix) > 0 && !strings.HasPrefix(file.Name(), prefix) {
				continue
			}
			filePath := filepath.Join(dirPath, file.Name())
			fmt.Printf("found file:%s, time:%s\n", filePath, file.ModTime().Format(time.RFC3339))
			if reallyDoIt {
				fmt.Println("remove file: ", filePath)
				if err := os.Remove(filePath); err != nil {
					return errors.As(err)
				}
			}
		}
		fmt.Println("Done")
		return nil
	},
}
var hlmDealMakeCmd = &cli.Command{
	Name:      "make-deal",
	Usage:     "Manually make a offline deal",
	ArgsUsage: "<list-file>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "cache-dir",
			Usage: "cache dir for cache the car data",
			Value: "/tmp",
		},
		&cli.Int64Flag{
			Name:  "src-max-size",
			Usage: "max source size, for 17GiB for 32GiB sector",
			Value: 17 * 1024 * 1024 * 1024,
		},
		&cli.IntFlag{
			Name:  "pack-interval",
			Usage: "pack interval in seconds",
			Value: 60,
		},
		&cli.StringFlag{
			Name:  "price",
			Usage: "price of deal",
			Value: "0",
		},
		&cli.IntFlag{
			Name:  "duration",
			Usage: "epochs of storage duration",
			Value: 1540800,
		},
		&cli.Int64Flag{
			Name:  "storage-id",
			Usage: "which storage will be used",
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "server-uri",
			Usage: "option, a full server uri path",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "from",
			Usage: "client address",
			Value: "",
		},
		&cli.BoolFlag{
			Name:  "manual",
			Usage: "import data in manual control",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		fnapi, fncloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer fncloser()

		api, closer, err := lcli.GetMarketsAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.DaemonContext(cctx)

		miner, err := api.ActorAddress(ctx)
		if err != nil {
			return errors.As(err)
		}

		fileListName := cctx.Args().First()
		if len(fileListName) == 0 {
			return errors.New("need input file list")
		}
		cacheDir := cctx.String("cache-dir")
		srcMaxSize := cctx.Int64("src-max-size")
		packInterval := time.Duration(cctx.Int("pack-interval")) * time.Second

		price, err := types.ParseFIL(cctx.String("price"))
		if err != nil {
			return errors.As(err)
		}
		dur := cctx.Int("duration")
		if abi.ChainEpoch(dur) < build.MinDealDuration {
			return xerrors.Errorf("minimum deal duration is %d blocks", build.MinDealDuration)
		}
		if abi.ChainEpoch(dur) > build.MaxDealDuration {
			return xerrors.Errorf("maximum deal duration is %d blocks", build.MaxDealDuration)
		}

		var faddr address.Address
		if from := cctx.String("from"); from != "" {
			a, err := address.NewFromString(from)
			if err != nil {
				return xerrors.Errorf("failed to parse 'from' address: %w", err)
			}
			faddr = a
		} else {
			def, err := fnapi.WalletDefaultAddress(ctx)
			if err != nil {
				return errors.As(err)
			}
			faddr = def
		}
		dcap, err := fnapi.StateVerifiedClientStatus(ctx, faddr, types.EmptyTSK)
		if err != nil {
			return errors.As(err)
		}
		isVerified := dcap != nil

		e := cidenc.Encoder{Base: multibase.MustNewEncoder(multibase.Base32)}
		packDeal := func(files []string) (string, *cid.Cid, *lapi.StartDealParams, error) {
			// make car
			carFile, err := unixfs.GenCarFile(ctx, cacheDir, "", files)
			if err != nil {
				return "", nil, nil, errors.As(err, files)
			}
			f, err := os.Open(carFile.Path)
			if err != nil {
				return "", nil, nil, errors.As(err, carFile.Path)
			}
			// generate the commp
			commp, err := unixfs.GenCommP(ctx, f)
			if err != nil {
				f.Close()
				return "", nil, nil, errors.As(err, carFile.Path)
			}
			f.Close()

			// make a deal
			ref := &storagemarket.DataRef{
				TransferType: storagemarket.TTManual,
				Root:         carFile.Roots[0],
				PieceCid:     &commp.PieceCID,
				PieceSize:    abi.UnpaddedPieceSize(commp.PieceSize.Unpadded()),
			}
			sdParams := &lapi.StartDealParams{
				Data:              ref,
				Wallet:            faddr,
				Miner:             miner,
				EpochPrice:        types.BigInt(price),
				MinBlocksDuration: uint64(dur),
				DealStartEpoch:    -1,
				FastRetrieval:     true,
				VerifiedDeal:      isVerified,
			}
			propCid, err := fnapi.ClientStatelessDeal(ctx, sdParams)
			if err != nil {
				return "", nil, nil, errors.As(err, files)
			}

			return carFile.Path, propCid, sdParams, nil
		}

		if cctx.Bool("manual") {
			path, propCid, params, err := packDeal([]string{fileListName})
			if err != nil {
				return errors.As(err)
			}
			output, err := json.Marshal(map[string]interface{}{
				"PropCid": e.Encode(*propCid),
				"Params":  params,
				"Path":    path,
			})
			if err != nil {
				return errors.As(err)
			}
			fmt.Println(string(output))
		} else {
			srcFile, err := os.Open(fileListName)
			if err != nil {
				return errors.As(err)
			}
			defer srcFile.Close()
			fileList := []string{}
			r := csv.NewReader(srcFile)
			for {
				record, err := r.Read()
				if err == io.EOF {
					break
				}
				if len(record) == 0 {
					continue
				}
				fileList = append(fileList, record[0])
			}

			// TODO: random files
			dealsNum := 0
			packIndex := 0
			fileListLen := len(fileList)
			for {
				packSize := int64(0)
				packFiles := []string{}
				packStartTime := time.Now()
				for ; packIndex < fileListLen; packIndex++ {
					if len(strings.TrimSpace(fileList[packIndex])) == 0 {
						continue
					}
					st, err := os.Stat(fileList[packIndex])
					if err != nil {
						return errors.As(err, fileList[packIndex])
					}
					packSize += st.Size()
					if packSize > srcMaxSize {
						packSize -= st.Size()
						break
					}
					packFiles = append(packFiles, fileList[packIndex])
				}
				if len(packFiles) == 0 {
					break
				}

				log.Infof("Begin package the origin file to car, endIndex:%d, num:%d, size:%d",
					packIndex, len(packFiles), packSize,
				)
				path, propCid, params, err := packDeal(packFiles)
				if err != nil {
					return errors.As(err)
				}
				defer os.Remove(path)
				pieceData := shared.PieceDataInfo{
					ReaderKind:    shared.PIECE_DATA_KIND_FILE,
					LocalPath:     path,
					ServerStorage: cctx.Int64("storage-id"),
					ServerFullUri: cctx.String("server-uri"),
					PropCid:       fmt.Sprintf("%s", propCid),
				}
				if err := api.DealsImportData(ctx, pieceData); err != nil {
					return errors.As(err, path)
				}
				paramsStr, err := json.Marshal(params)
				if err != nil {
					return errors.As(err)
				}
				used := time.Now().Sub(packStartTime)
				log.Infof("End package the origin file to car, num:%d, used:%s, propid:%s,params:%s",
					len(packFiles),
					used,
					e.Encode(*propCid), paramsStr,
				)
				dealsNum++

				// sleep for next
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(packInterval - used):
				}
			}
			log.Infof("All offline deals(%d) commit done", len(fileList))
		}
		return nil

	},
}
var hlmDealPathCmd = &cli.Command{
	Name:  "new-fstmp",
	Usage: "make a random tmp path",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "create random tmp path",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := lcli.DaemonContext(cctx)
		if !cctx.Bool("really-do-it") {
			return errors.New("need a really-do-it for touch file")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		path, err := nodeApi.NewMarketDealFSTMP(ctx)
		if err != nil {
			return errors.As(err)
		}
		fmt.Println(path)
		return nil
	},
}
var hlmDealRecordCmd = &cli.Command{
	Name:      "record-deal",
	Usage:     "record deal info to db table",
	ArgsUsage: "<prop_id> <root_cid> <piece_cid> <piece_unpadded_size> <client_addr> <file_local> <file_remote> <file_storage>",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.DaemonContext(cctx)
		args := cctx.Args()
		if args.Len() != 8 {
			fmt.Printf("need arguments %s\n", cctx.Command.Usage)
			return nil
		}
		propID := args.Get(0)
		rootCid := args.Get(1)
		pieceCid := args.Get(2)
		pieceSizeStr := args.Get(3)
		clientAddr := args.Get(4)
		fileLocal := args.Get(5)
		fileRemote := args.Get(6)
		fileStorageStr := args.Get(7)
		pieceSize, err := strconv.ParseInt(pieceSizeStr, 10, 64)
		if err != nil {
			return errors.As(err, pieceSizeStr)
		}
		fileStorage, err := strconv.ParseInt(fileStorageStr, 10, 64)
		if err != nil {
			return errors.As(err, pieceSizeStr)
		}

		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		return nodeApi.AddMarketDeal(ctx, &database.MarketDealInfo{
			ID:          propID,
			UpdatedAt:   time.Now(),
			RootCid:     rootCid,
			PieceCid:    pieceCid,
			PieceSize:   pieceSize,
			ClientAddr:  clientAddr,
			FileLocal:   fileLocal,
			FileRemote:  fileRemote,
			FileStorage: fileStorage,
		})
	},
}
var hlmDealInfoCmd = &cli.Command{
	Name:      "get-deal",
	Usage:     "get a db deal information",
	ArgsUsage: "<prop_id>",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.DaemonContext(cctx)
		args := cctx.Args()
		if !args.Present() {
			return errors.New("need input <prop_id>")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		info, err := nodeApi.GetMarketDeal(ctx, args.Get(0))
		if err != nil {
			return errors.As(err)
		}
		output, err := json.MarshalIndent(info, "", "	")
		if err != nil {
			return errors.As(err)
		}
		fmt.Println(string(output))
		return nil
	},
}
var hlmDealSectorCmd = &cli.Command{
	Name:      "sector-deal",
	Usage:     "get deals of sector",
	ArgsUsage: "<sector-name>",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.DaemonContext(cctx)
		args := cctx.Args()
		if !args.Present() {
			return errors.New("need input <sector-name>")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		info, err := nodeApi.GetMarketDealBySid(ctx, args.Get(0))
		if err != nil {
			return errors.As(err)
		}
		output, err := json.Marshal(info)
		if err != nil {
			return errors.As(err)
		}
		fmt.Println(string(output))
		return nil
	},
}
