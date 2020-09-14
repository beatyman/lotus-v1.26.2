package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var testCmd = &cli.Command{
	Name:  "test",
	Usage: "test command",
	Subcommands: []*cli.Command{
		testWdPoStCmd,
	},
}

var testWdPoStCmd = &cli.Command{
	Name:  "wdpost",
	Usage: "testing wdpost",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "index",
			Value: 0,
			Usage: "Window PoSt deadline index",
		},
		&cli.BoolFlag{
			Name:  "check-sealed",
			Value: false,
			Usage: "check and relink the sealed file",
		},
		&cli.BoolFlag{
			Name:  "mount",
			Value: false,
			Usage: "mount storage node from miner",
		},
		&cli.BoolFlag{
			Name:  "do-wdpost",
			Value: true,
			Usage: "running window post, false only check sectors files",
		},
	},
	Action: func(cctx *cli.Context) error {
		minerRepo, err := homedir.Expand(cctx.String("miner-repo"))
		if err != nil {
			return err
		}
		database.InitDB(filepath.Join(minerRepo))

		fullApi, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()
		minerApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()

		ctx, cancel := context.WithCancel(lcli.ReqContext(cctx))
		defer cancel()

		act, err := minerApi.ActorAddress(ctx)
		if err != nil {
			return err
		}
		ssize, err := minerApi.ActorSectorSize(ctx, act)
		if err != nil {
			return err
		}
		log.Infof("Running ActorSize:%s", ssize.ShortString())

		spt, err := ffiwrapper.SealProofTypeFromSectorSize(ssize)
		if err != nil {
			return xerrors.Errorf("getting proof type: %w", err)
		}
		cfg := &ffiwrapper.Config{
			SealProofType: spt,
		}

		minerSealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: minerRepo,
		}, cfg)
		if err != nil {
			return err
		}
		// mount storage from miner
		if cctx.Bool("mount") {
			rs := &rpcServer{
				sb:           minerSealer,
				storageCache: map[int64]database.StorageInfo{},
			}
			if err := rs.loadMinerStorage(ctx, minerApi); err != nil {
				return errors.As(err)
			}
		}

		di := dline.Info{
			Index: cctx.Uint64("index"), // TODO: get from params
		}
		ts, err := fullApi.ChainHead(ctx)
		if err != nil {
			return err
		}
		partitions, err := fullApi.StateMinerPartitions(ctx, act, di.Index, ts.Key())
		if err != nil {
			return errors.As(err)
		}
		if len(partitions) == 0 {
			fmt.Println("No partitions")
			return nil
		}

		buf := new(bytes.Buffer)
		if err := act.MarshalCBOR(buf); err != nil {
			return errors.As(err)
		}

		rand, err := fullApi.ChainGetRandomnessFromBeacon(ctx, ts.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes())
		if err != nil {
			return errors.As(err)
		}

		mid, err := address.IDFromAddress(act)
		if err != nil {
			return errors.As(err)
		}
		var sinfos []proof.SectorInfo
		var sectors = []abi.SectorID{}
		for partIdx, partition := range partitions {
			pSector := partition.Sectors
			liveCount, err := pSector.Count()
			if err != nil {
				return errors.As(err)
			}
			sset, err := fullApi.StateMinerSectors(ctx, act, &pSector, false, ts.Key())
			if err != nil {
				return errors.As(err, partIdx)
			}
			fmt.Printf("partition:%d,sectors:%d, sset:%d\n", partIdx, liveCount, len(sset))
			for _, sector := range sset {
				sectors = append(sectors, abi.SectorID{
					Miner:  abi.ActorID(mid),
					Number: sector.ID,
				})
				sinfos = append(sinfos, proof.SectorInfo{
					SectorNumber: sector.ID,
					SealedCID:    sector.Info.SealedCID,
					SealProof:    sector.Info.SealProof,
				})
			}
		}
		fmt.Println("Start CheckProvable")
		start := time.Now()
		all, bad, err := ffiwrapper.CheckProvable(minerRepo, ssize, sectors, 6*time.Second)
		if err != nil {
			return errors.As(err)
		}

		toProvInfo := []proof.SectorInfo{}
		for _, val := range all {
			errStr := "nil"
			if err := errors.ParseError(val.Err); err != nil {
				errStr = err.Code()
			} else {
				for i, _ := range sectors {
					if sectors[i].Number == val.ID.Number {
						toProvInfo = append(toProvInfo, sinfos[i])
						break
					}
				}
			}
			fmt.Printf("s-t0%d-%d,%d,%s,%+v\n", val.ID.Miner, val.ID.Number, val.Used, val.Used.String(), errStr)
		}
		fmt.Printf("used:%s,all:%d, bad:%d,toProve:%d\n", time.Now().Sub(start).String(), len(all), len(bad), len(toProvInfo))
		//	for _, val := range toProvInfo {
		//		fmt.Println(val.SectorNumber)
		//	}
		if !cctx.Bool("do-wdpost") {
			return nil
		}

		if _, _, err := minerSealer.GenerateWindowPoSt(ctx, abi.ActorID(mid), toProvInfo, abi.PoStRandomness(rand)); err != nil {
			return errors.As(err)
		}
		return nil
	},
}
