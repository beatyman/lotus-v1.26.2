package ffiwrapper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

type ProvableStat struct {
	Sector storage.SectorFile
	Used   time.Duration
	Err    error
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
func CheckProvable(ctx context.Context, ssize abi.SectorSize, sectors []storage.SectorFile, timeout time.Duration) ([]ProvableStat, []ProvableStat, []ProvableStat, error) {
	log.Info("Manager.CheckProvable in, len:", len(sectors))
	defer log.Info("Manager.CheckProvable out, len:", len(sectors))

	var good = []ProvableStat{}
	var goodLk = sync.Mutex{}
	var appendGood = func(sid ProvableStat) {
		goodLk.Lock()
		defer goodLk.Unlock()
		good = append(good, sid)
	}

	var bad = []ProvableStat{}
	var badLk = sync.Mutex{}
	var appendBad = func(sid ProvableStat) {
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

	checkBad := func(ctx context.Context, sector storage.SectorFile) error {
		if len(sector.StorageRepo) == 0 {
			return errors.New("StorageRepo not found").As(sector)
		}
		lp := storiface.SectorPaths{
			ID:       sector.SectorID(),
			Unsealed: sector.UnsealedFile(),
			Sealed:   sector.SealedFile(),
			Cache:    sector.CachePath(),
		}

		if lp.Sealed == "" || lp.Cache == "" {
			return errors.New("CheckProvable Sector FAULT: cache an/or sealed paths not found").As(sector, lp.Sealed, lp.Cache)
		}

		toCheck := map[string]int64{
			lp.Sealed:                        int64(ssize),
			//filepath.Join(lp.Cache, "t_aux"): 0, // no check for fake
			filepath.Join(lp.Cache, "p_aux"): 0,
		}

		addCachePathsForSectorSize(toCheck, lp.Cache, ssize)

		for p, sz := range toCheck {
			// checking data
			checkDone := make(chan error, 1)
			go func() {
				st, err := os.Stat(p)
				if err != nil {
					checkDone <- errors.As(err, p)
					return
				}

				if sz != 0 {
					if st.Size() < sz {
						checkDone <- errors.New("CheckProvable Sector FAULT: sector file is wrong size").As(p, st.Size())
						return
					}
				}
				checkDone <- nil
				return
			}()

			select {
			case <-ctx.Done():
				return errors.New("context canceled").As(p)
			case err := <-checkDone:
				if err != nil {
					return errors.As(err, p)
				}
				// continue
			}
		}
		return nil
	}
	// limit the gorouting to checking the bad, the sectors would be so a lots.
	// limit low donw to 256 cause by 'runtime/cgo: pthread_create failed: Resource temporarily unavailable' panic
	routines := make(chan bool, 256)
	done := make(chan bool, len(sectors))
	for _, sector := range sectors {
		go func(s storage.SectorFile) {
			// limit the concurrency
			routines <- true
			defer func() {
				<-routines
			}()

			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()
			err := checkBad(checkCtx, s)
			used := time.Now().Sub(start)
			pState := ProvableStat{Sector: s, Used: used, Err: err}
			if err != nil {
				appendBad(pState)
			} else {
				appendGood(pState)
			}
			appendAll(pState)

			// thread end
			done <- true
		}(sector)
	}
	for waits := len(sectors); waits > 0; waits-- {
		<-done
	}

	sort.Sort(all)
	return all, good, bad, nil
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
		// chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", 0))] = 4586976 // just check one file cause it use a lots of time to check every files.
		for i := 0; i < 8; i++ {
			//chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 4586976
			chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 0
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
