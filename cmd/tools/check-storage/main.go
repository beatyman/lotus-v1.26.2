package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

var (
	startFlag = flag.Int64("start", 0, "the sector started")
	endFlag   = flag.Int64("end", 2300, "the sector ended")
	minerId   = flag.Int64("miner-id", 1003, "the int of miner id")
	repoFlag  = flag.String("repo", "/data/sdb/lotus-user-1/.lotusstorage", "the miner repo")
)

func main() {
	flag.Parse()
	fmt.Printf("miner:%d,start:%d,end:%d\n", minerId, startFlag, endFlag)
	// TODO: get sectors from partitions
	sectors := []abi.SectorID{}
	for i := *startFlag; i <= *endFlag; i++ {
		sectors = append(sectors, abi.SectorID{
			Miner:  abi.ActorID(*minerId),
			Number: abi.SectorNumber(i),
		})
	}

	repo := *repoFlag
	ssize := abi.SectorSize(32 << 30)
	start := time.Now()
	all, bad, err := CheckProvable(repo, ssize, sectors, 3*time.Second)
	if err != nil {
		panic(err)
	}

	for _, val := range all {
		errStr := "nil"
		if err := errors.ParseError(val.Err); err != nil {
			errStr = err.Code()
		}
		fmt.Printf("s-t0%d-%d,%d,%s,%+v\n", *minerId, val.ID.Number, val.Used, val.Used.String(), errStr)
	}
	fmt.Printf("used:%s, bad:%d\n", time.Now().Sub(start).String(), len(bad))
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
