package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/urfave/cli/v2"

	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/gwaylib/errors"
)

var hlmStorageCmd = &cli.Command{
	Name:  "hlm-storage",
	Usage: "Manage storage",
	Subcommands: []*cli.Command{
		verHLMStorageCmd,
		getHLMStorageCmd,
		searchHLMStorageCmd,
		addHLMStorageCmd,
		disableHLMStorageCmd,
		enableHLMStorageCmd,
		statusHLMStorageCmd,
		mountHLMStorageCmd,
		relinkHLMStorageCmd,
		replaceHLMStorageCmd,
		scaleHLMStorageCmd,
		setHLMStorageTimeoutCmd,
		getHLMStorageTimeoutCmd,
	},
}
var verHLMStorageCmd = &cli.Command{
	Name:      "ver",
	Usage:     "get the current max version of the storage",
	ArgsUsage: "id",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		ver, err := nodeApi.VerHLMStorage(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("max ver:%d\n", ver)
		return nil
	},
}
var getHLMStorageCmd = &cli.Command{
	Name:      "get",
	Usage:     "get a storage node information",
	ArgsUsage: "id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() == 0 {
			return errors.New("need input id")
		}
		id, err := strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return err
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		info, err := nodeApi.GetHLMStorage(ctx, id)
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

var searchHLMStorageCmd = &cli.Command{
	Name:      "search",
	Usage:     "search a storage node information by signal ip",
	ArgsUsage: "ip",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		ip := args.First()
		if len(ip) == 0 {
			return errors.New("need input ip")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		info, err := nodeApi.SearchHLMStorage(ctx, ip)
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

var addHLMStorageCmd = &cli.Command{
	Name:  "add",
	Usage: "add a storage node",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "kind",
			Usage: "0, for sealed storage; 1 for unsealed storage",
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "mount-type",
			Usage: "mount type, like nfs, empty for local folder by default.",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-signal-uri",
			Usage: "uri for mount signal channel, net uri or local uri",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-transf-uri",
			Usage: "uri for mount transfer channel, net uri or local uri",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-dir",
			Usage: "parent dir of mount point",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-opt",
			Usage: "mount opt, format should be \"-o ...\"",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-auth",
			Usage: "mount auth, format should be a token",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-auth-uri",
			Usage: "mount auth uri, format should be \"ip:port\"",
			Value: "",
		},
		&cli.Int64Flag{
			Name:  "max-size",
			Usage: "storage max size, in byte",
			Value: 0,
		},
		&cli.Int64Flag{
			Name:  "keep-size",
			Usage: "the storage should keep size for other, in byte",
			Value: 0,
		},
		&cli.Int64Flag{
			Name:  "sector-size",
			Usage: "sector size, the result sizes of sealed+cache, default is 100GB",
			Value: 107374182400,
		},

		&cli.IntFlag{
			Name:  "max-work",
			Usage: "the max number currency work",
			Value: 5,
		},
	},
	Action: func(cctx *cli.Context) error {
		kind := cctx.Int("kind")
		switch kind {
		case database.STORAGE_KIND_SEALED, database.STORAGE_KIND_UNSEALED,database.STORAGE_KIND_MARKET:
		default:
			fmt.Printf("unknow kind:%d\n", kind)
			return nil
		}
		mountType := cctx.String("mount-type")
		mountOpt := cctx.String("mount-opt")
		mountSignalUri := cctx.String("mount-signal-uri")
		mountTransfUri := cctx.String("mount-transf-uri")

		mountAuthUri := cctx.String("mount-auth-uri")
		mountAuth := cctx.String("mount-auth")
		switch mountType {
		case database.MOUNT_TYPE_HLM:
			if len(mountSignalUri) == 0 {
				return errors.New("need mount-signal-uri")
			}
			if len(mountTransfUri) == 0 {
				return errors.New("need mount-transf-uri")
			}
			if len(mountAuthUri) == 0 {
				return errors.New("need mount-auth-uri")
			}
		default:
			if len(mountSignalUri) == 0 {
				return errors.New("need mount-signal-uri")
			}
			if len(mountTransfUri) == 0 {
				mountTransfUri = mountSignalUri
			}
		}

		mountDir := cctx.String("mount-dir")
		if len(mountDir) == 0 {
			return errors.New("need mount-dir")
		}
		maxSize := cctx.Int64("max-size")
		if maxSize < -1 {
			return errors.New("need max-size")
		}

		keepSize := cctx.Int64("keep-size")
		sectorSize := cctx.Int64("sector-size")
		maxWork := cctx.Int("max-work")

		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.AddHLMStorage(ctx, &database.StorageAuth{
			StorageInfo: database.StorageInfo{
				Kind:           kind,
				MountType:      mountType,
				MountSignalUri: mountSignalUri,
				MountTransfUri: mountTransfUri,
				MountDir:       mountDir,
				MountOpt:       mountOpt,
				MountAuthUri:   mountAuthUri,
				MaxSize:        maxSize,
				KeepSize:       keepSize,
				SectorSize:     sectorSize,
				MaxWork:        maxWork,
				Version:        time.Now().UnixNano(),
				MountAuth:      mountAuth,
			},
		})
	},
}

var disableHLMStorageCmd = &cli.Command{
	Name:      "disable",
	Usage:     "Disable a storage node to stop allocating for only read",
	ArgsUsage: "id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() == 0 {
			return errors.New("need input id")
		}
		id, err := strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return err
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.DisableHLMStorage(ctx, id, true)
	},
}
var enableHLMStorageCmd = &cli.Command{
	Name:      "enable",
	Usage:     "Enable a storage node to recover allocating for write",
	ArgsUsage: "id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() == 0 {
			return errors.New("need input id")
		}
		id, err := strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return err
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.DisableHLMStorage(ctx, id, false)
	},
}
var mountHLMStorageCmd = &cli.Command{
	Name:      "mount",
	Usage:     "Mount a storage by node id, if exist, will remount it.",
	ArgsUsage: "id/all",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() == 0 {
			return errors.New("need input id")
		}
		storageId := int64(0)
		input := args.First()
		if input != "all" {
			id, err := strconv.ParseInt(args.First(), 10, 64)
			if err != nil {
				return err
			}
			storageId = id
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.MountHLMStorage(ctx, storageId)
	},
}

var relinkHLMStorageCmd = &cli.Command{
	Name:      "relink",
	Usage:     "Relink(ln -s) the cache and sealed to the storage node",
	ArgsUsage: "id -- storage id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		if args.Len() == 0 {
			return errors.New("need input storage id")
		}
		firstArg := args.First()
		id := int64(0)
		if firstArg != "all" {
			stroageId, err := strconv.ParseInt(firstArg, 10, 64)
			if err != nil {
				return err
			}
			id = stroageId
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		maddr, err := nodeApi.ActorAddress(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("======== relink miner: %+v,sectors from storage nodes ==========\n",maddr.String())
		return nodeApi.RelinkHLMStorage(ctx, id,maddr.String())
	},
}

var replaceHLMStorageCmd = &cli.Command{
	Name:  "replace",
	Usage: "Replace the storage node",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:  "storage-id",
			Usage: "id of storage",
		},
		&cli.StringFlag{
			Name:  "mount-type",
			Usage: "mount type, like nfs, empty to keep the origin value",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-signal-uri",
			Usage: "uri for mount signal uri, net uri or local uri who can mount",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-transf-uri",
			Usage: "uri for mount transfer uri, net uri or local uri who can mount, empty should same as mount-signal-uri",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-auth-uri",
			Usage: "uri for mount auth uri, net uri or local uri who can mount, empty should keep the old one",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-opt",
			Usage: "mount opt, format should be \"-o ...\", empty to keep the origin value",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "mount-auth",
			Usage: "mount auth, format should be \"-o ...\", empty to keep the origin value",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		storageId := cctx.Int64("storage-id")
		if storageId <= 0 {
			return errors.New("need input storage-id>0")
		}

		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		info, err := nodeApi.GetHLMStorage(ctx, storageId)
		if err != nil {
			return err
		}
		mountType := cctx.String("mount-type")
		if len(mountType) > 0 {
			info.MountType = mountType
		}
		mountOpt := cctx.String("mount-opt")
		if len(mountOpt) > 0 {
			info.MountOpt = mountOpt
		}
		mountAuth := cctx.String("mount-auth")
		mountSignalUri := cctx.String("mount-signal-uri")
		mountTransfUri := cctx.String("mount-transf-uri")
		mountAuthUri := cctx.String("mount-auth-uri")
		info.MountAuth = mountAuth
		switch info.MountType {
		case database.MOUNT_TYPE_HLM:
			if len(mountSignalUri) == 0 {
				return errors.New("need mount-signal-uri")
			}
			if len(mountTransfUri) == 0 {
				return errors.New("need mount-transf-uri")
			}
			if len(mountAuthUri) == 0 {
				return errors.New("need mount-auth-uri")
			}
			info.MountSignalUri = mountSignalUri
			info.MountTransfUri = mountTransfUri
			info.MountAuthUri = mountAuthUri
		default:
			if len(mountSignalUri) == 0 {
				return errors.New("need mount-signal-uri")
			}
			if len(mountTransfUri) == 0 {
				mountTransfUri = mountSignalUri
			}
			info.MountSignalUri = mountSignalUri
			info.MountTransfUri = mountTransfUri
		}
		return nodeApi.ReplaceHLMStorage(ctx, &database.StorageAuth{StorageInfo: *info})
	},
}

var scaleHLMStorageCmd = &cli.Command{
	Name:  "scale",
	Usage: "scale storage maxSize OR maxWork by node id ",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "storage-id",
			Usage: "storage ID",
			Value: "",
		},
		&cli.Int64Flag{
			Name:  "max-size",
			Usage: "storage max size, in byte",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "max-work",
			Usage: "the max number currency work",
			Value: 0,
		},
	},
	Action: func(cctx *cli.Context) error {
		storageId, err := strconv.ParseInt(cctx.String("storage-id"), 10, 64)
		if err != nil {
			return err
		}
		if storageId < 1 {
			return errors.New("storageId need input > 1")
		}
		maxSize, err := strconv.ParseInt(cctx.String("max-size"), 10, 64)
		if err != nil {
			return err
		}
		if maxSize < -1 {
			return errors.New("maxSize need input >= -1")
		}
		maxWork, err := strconv.ParseInt(cctx.String("max-work"), 10, 64)
		if err != nil {
			return err
		}
		if maxWork < 0 {
			return errors.New("maxWork need input >= 0")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.ScaleHLMStorage(ctx, storageId, maxSize, maxWork)
	},
}

var statusHLMStorageCmd = &cli.Command{
	Name:  "status",
	Usage: "the storage nodes status",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "output the normal sector message",
			Value: false,
		},
		&cli.Int64Flag{
			Name:  "storage-id",
			Usage: "storage ID",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "timeout",
			Usage: "timeout for every node. Uint is in second",
			Value: 6,
		},
	},
	Action: func(cctx *cli.Context) error {
		storageId := cctx.Int64("storage-id")
		if storageId < 0 {
			return errors.New("error storage id")
		}
		timeout := cctx.Int("timeout")
		if timeout < 1 {
			return errors.New("error timeout")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		stats, err := nodeApi.StatusHLMStorage(ctx, storageId, time.Duration(timeout)*time.Second)
		if err != nil {
			return err
		}
		debug := cctx.Bool("debug")
		good := []database.StorageStatus{}
		bad := []database.StorageStatus{}
		disable := []database.StorageStatus{}
		fmt.Println("======== storage nodes ==========")
		for _, stat := range stats {
			if debug {
				fmt.Printf("%+v\n", stat)
			}
			if stat.Disable {
				fmt.Printf("disable node, id:%d, uri:%s\n", stat.StorageId, stat.MountUri)
				disable = append(disable, stat)
				continue
			}
			if len(stat.Err) > 0 {
				fmt.Printf("bad node,     id:%d, uri:%s, used:%s, err:\n%s\n", stat.StorageId, stat.MountUri, stat.Used, errors.Parse(stat.Err).Code())
				bad = append(bad, stat)
				continue
			}
			good = append(good, stat)
		}
		fmt.Printf("all:%d, good:%d, bad:%d, disable:%d\n", len(stats), len(good), len(bad), len(disable))
		fmt.Println("=================================")
		fmt.Println("=========== miner node ==========")
		minerStorageStatus, err := nodeApi.StatusMinerStorage(ctx)
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Print(string(minerStorageStatus))
		}
		fmt.Println("=================================")
		return nil
	},
}
var setHLMStorageTimeoutCmd = &cli.Command{
	Name:  "set-timeout",
	Usage: "set the timeout of storage checking",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "fault-timeout",
			Usage: "timeout of declare fault, unit is seconds, not change by 0",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "proving-timeout",
			Usage: "timeout of declare fault, unit is seconds, not change by 0",
			Value: 0,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		// set
		pTimeout := cctx.Int("proving-timeout")
		if pTimeout > 0 {
			if err := nodeApi.SetProvingCheckTimeout(ctx, time.Duration(pTimeout)*time.Second); err != nil {
				return err
			}
			fmt.Println("done proving timeout set")
		}
		fTimeout := cctx.Int("fault-timeout")
		if fTimeout > 0 {
			if err := nodeApi.SetProvingCheckTimeout(ctx, time.Duration(fTimeout)*time.Second); err != nil {
				return err
			}
			fmt.Println("done fault timeout set")
		}
		return nil
	},
}
var getHLMStorageTimeoutCmd = &cli.Command{
	Name:  "get-timeout",
	Usage: "get the timeout of storage checking",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		pTimeout, err := nodeApi.GetProvingCheckTimeout(ctx)
		if err != nil {
			return err
		}
		fTimeout, err := nodeApi.GetFaultCheckTimeout(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("proving check:%s, fault declare:%s\n", pTimeout.String(), fTimeout.String())
		return nil
	},
}
