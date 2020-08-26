package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/crypto"
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
			Name:  "mount",
			Value: false,
			Usage: "mount storage node from miner",
		},
	},
	Action: func(cctx *cli.Context) error {
		minerRepo, err := homedir.Expand(cctx.String("miner-repo"))
		if err != nil {
			return err
		}

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
			if err := rs.loadMinerStorage(ctx); err != nil {
				return errors.As(err)
			}
		}

		di := miner.DeadlineInfo{
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
		var sinfos []abi.SectorInfo
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
				sinfos = append(sinfos, abi.SectorInfo{
					SectorNumber: sector.ID,
					SealedCID:    sector.Info.SealedCID,
					SealProof:    sector.Info.SealProof,
				})
			}
		}
		start := time.Now()
		all, bad, err := CheckProvable(minerRepo, ssize, sectors, 3*time.Second)
		if err != nil {
			return errors.As(err)
		}

		toProvInfo := []abi.SectorInfo{}
		for i, val := range all {
			errStr := "nil"
			if err := errors.ParseError(val.Err); err != nil {
				errStr = err.Code()
			} else {
				toProvInfo = append(toProvInfo, sinfos[i])
			}
			fmt.Printf("s-t0%d-%d,%d,%s,%+v\n", val.ID.Miner, val.ID.Number, val.Used, val.Used.String(), errStr)
		}
		fmt.Printf("used:%s,all:%d, bad:%d\n", time.Now().Sub(start).String(), len(all), len(bad))

		if _, _, err := minerSealer.GenerateWindowPoSt(ctx, abi.ActorID(mid), toProvInfo, abi.PoStRandomness(rand)); err != nil {
			return errors.As(err)
		}
		return nil
	},
}

type ProvableStat struct {
	ID   abi.SectorID
	Used time.Duration
	Err  error
}

type ProvableStatArr []ProvableStat

func (g ProvableStatArr) Len() int {
	return len(g)
}
func (g ProvableStatArr) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}
func (g ProvableStatArr) Less(i, j int) bool {
	return g[i].Used < g[j].Used
}

// CheckProvable returns unprovable sectors
func CheckProvable(repo string, ssize abi.SectorSize, sectors []abi.SectorID, timeout time.Duration) (ProvableStatArr, []abi.SectorID, error) {
	log.Info("Manager.CheckProvable in, len:", len(sectors))
	defer log.Info("Manager.CheckProvable out, len:", len(sectors))
	var bad = []abi.SectorID{}
	var badLk = sync.Mutex{}
	var appendBad = func(sid abi.SectorID) {
		badLk.Lock()
		defer badLk.Unlock()
		bad = append(bad, sid)
	}

	var all = ProvableStatArr{}
	var allLk = sync.Mutex{}
	var appendAll = func(good ProvableStat) {
		allLk.Lock()
		defer allLk.Unlock()
		all = append(all, good)
	}

	checkBad := func(sector abi.SectorID) error {
		lp := stores.SectorPaths{
			ID:       sector,
			Unsealed: filepath.Join(repo, "unsealed", ffiwrapper.SectorName(sector)),
			Sealed:   filepath.Join(repo, "sealed", ffiwrapper.SectorName(sector)),
			Cache:    filepath.Join(repo, "cache", ffiwrapper.SectorName(sector)),
		}

		if lp.Sealed == "" || lp.Cache == "" {
			return errors.New("CheckProvable Sector FAULT: cache an/or sealed paths not found").As(sector, lp.Sealed, lp.Cache)
		}

		toCheck := map[string]int64{
			lp.Sealed:                        int64(ssize),
			filepath.Join(lp.Cache, "t_aux"): 0,
			filepath.Join(lp.Cache, "p_aux"): 0,
		}

		addCachePathsForSectorSize(toCheck, lp.Cache, ssize)

		for p, sz := range toCheck {
			st, err := os.Stat(p)
			if err != nil {
				return errors.As(err, p)
			}

			if sz != 0 {
				if st.Size() < sz {
					return errors.New("CheckProvable Sector FAULT: sector file is wrong size").As(p, st.Size())
				}
			}
			// TODO: check 'sanity check failed' in rust
		}
		return nil
	}
	routines := make(chan bool, 1024) // limit the gorouting to checking the bad, the sectors would be lot.
	done := make(chan bool, len(sectors))
	for _, sector := range sectors {
		go func(s abi.SectorID) {
			// limit the concurrency
			routines <- true
			defer func() {
				<-routines
			}()

			// checking data
			checkDone := make(chan error, 1)
			start := time.Now()
			go func() {
				checkDone <- checkBad(s)
			}()
			var errResult error
			select {
			case <-time.After(timeout):
				// read sector timeout
				errResult = errors.New("sector stat timeout").As(timeout)
			case err := <-checkDone:
				errResult = err
			}
			used := time.Now().Sub(start)
			if errResult != nil {
				appendBad(s)
			}
			appendAll(ProvableStat{ID: s, Used: used, Err: errResult})

			// thread end
			done <- true
		}(sector)
	}
	for waits := len(sectors); waits > 0; waits-- {
		<-done
	}

	sort.Sort(all)
	return all, bad, nil
}

func addCachePathsForSectorSize(chk map[string]int64, cacheDir string, ssize abi.SectorSize) {
	switch ssize {
	case 2 << 10:
		fallthrough
	case 8 << 20:
		fallthrough
	case 512 << 20:
		chk[filepath.Join(cacheDir, "sc-02-data-tree-r-last.dat")] = 0
	case 32 << 30:
		// chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", 0))] = 4586976 // just check one file cause it use a lot of time to check every file.
		for i := 0; i < 8; i++ {
			chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 4586976
		}
	case 64 << 30:
		//chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", 0))] = 4586976
		for i := 0; i < 16; i++ {
			chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 0
		}
	default:
		log.Warnf("not checking cache files of %s sectors for faults", ssize)
	}
}
