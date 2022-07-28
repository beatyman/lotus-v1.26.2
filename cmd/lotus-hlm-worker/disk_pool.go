package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/gwaylib/errors"
)

const (
	DISK_MOUNT_ROOT  = "/data/lotus-cache"
	SECTOR_CACHE_DIR = "cache"
)

type DiskPool interface {
	// return all the repos
	Repos() []string

	// allocate disk and return repo
	//
	// return
	// repo -- like /data/lotus-cache/1
	Allocate(sid string) (SectorState, error)
	UpdateState(sid string, state int) error

	// query sid's cache directory with sector id
	Query(sid string) (SectorState, error)

	// delete disk data with sector id
	Delete(sid string) error

	ShowExt() ([]string, error)
}

type diskinfo struct {
	mnt string
	ds  DiskStatus
}

type SectorState struct {
	MountPoint string
	State      int
}

type diskPoolImpl struct {
	mutex       sync.Mutex
	ssize       abi.SectorSize
	workerCfg   ffiwrapper.WorkerCfg
	defaultRepo string

	sectors map[string]SectorState // sid:moint_point
	hasDisk bool
}

// defaultRepo -- if disk pool not found, use the old repo for work.
func NewDiskPool(ssize abi.SectorSize, workerCfg ffiwrapper.WorkerCfg, defaultRepo string) DiskPool {
	return &diskPoolImpl{
		ssize:       ssize,
		workerCfg:   workerCfg,
		defaultRepo: defaultRepo,
		sectors:     map[string]SectorState{},
	}
}

const (
	B  = 1
	KB = 1024 * B
	MB = 1024 * KB
	GB = 1024 * MB
	TB = 1024 * GB
	PB = 1024 * TB
	EB = 1024 * PB
	ZB = 1024 * EB
	YB = 1024 * ZB
)

type DiskStatus struct {
	SectorSize uint64 `json:"sectorsize"`
	All        uint64 `json:"all"`
	Used       uint64 `json:"used"`
	Free       uint64 `json:"free"`
	MaxSector  uint64 `json:"maxsector"`
	UsedSector uint64 `json:"usedsector"`
}

func sectorCap(total, ssize uint64) uint64 {
	switch ssize {
	case 64 * GB:
		return total / (1000 * GB)
	case 32 * GB:
		return total / (500 * GB)
	case 512 * MB:
		return total / (8000 * MB)
	case 8 * MB:
		return total / (125 * KB)
	case 2 * KB:
		// const this value, so, it should easy to test.
		return 4
		// return total / (100 * KB)
	default:
		return 0
	}
}

// disk usage of path/disk
func DiskUsage(path string, ssize uint64) (*DiskStatus, error) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return nil, errors.As(err, path, ssize)
	}

	all := fs.Blocks * uint64(fs.Bsize)
	free := fs.Bfree * uint64(fs.Bsize)
	return &DiskStatus{
		SectorSize: ssize,
		All:        all,
		Free:       free,
		Used:       all - free,
		MaxSector:  sectorCap(all, ssize),
	}, nil
}

// 字节的单位转换 保留两位小数
func formatFileSize(fileSize uint64) (size string) {
	if fileSize < KB {
		return fmt.Sprintf("%.2fB", float64(fileSize)/float64(B))
	} else if fileSize < MB {
		return fmt.Sprintf("%.2fKB", float64(fileSize)/float64(KB))
	} else if fileSize < GB {
		return fmt.Sprintf("%.2fMB", float64(fileSize)/float64(MB))
	} else if fileSize < TB {
		return fmt.Sprintf("%.2fGB", float64(fileSize)/float64(GB))
	} else if fileSize < PB {
		return fmt.Sprintf("%.2fTB", float64(fileSize)/float64(TB))
	} else if fileSize < EB {
		return fmt.Sprintf("%.2fPB", float64(fileSize)/float64(PB))
	} else {
		return fmt.Sprintf("%.2fEB", float64(fileSize)/float64(EB))
	}
}

func IsMountPoint(dir string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), 30e9)
	defer cancel()
	output, err := exec.CommandContext(ctx, "mount", "-l").CombinedOutput()
	if err != nil {
		return false, errors.As(err, dir)
	}
	return strings.Contains(string(output), dir), nil
}

func scanRepo(repo string) map[string]bool {
	result := map[string]bool{}

	dir := filepath.Join(repo, "unsealed")
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Warn(err)
	} else {
		for _, file := range files {
			result[file.Name()] = true
		}
	}

	dir = filepath.Join(repo, "sealed")
	files, err = ioutil.ReadDir(dir)
	if err != nil {
		log.Warn(err)
	} else {
		for _, file := range files {
			result[file.Name()] = true
		}
	}

	dir = filepath.Join(repo, "cache")
	files, err = ioutil.ReadDir(dir)
	if err != nil {
		log.Warn(err)
	} else {
		for _, file := range files {
			result[file.Name()] = true
		}
	}
	return result
}

func scanDisk() (map[string]map[string]bool, error) {
	if err := os.MkdirAll(DISK_MOUNT_ROOT, 0755); err != nil {
		return nil, errors.As(err)
	}
	dir := filepath.Join(DISK_MOUNT_ROOT)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, errors.As(err)
	}

	result := map[string]map[string]bool{}
	for _, file := range files {
		repo := filepath.Join(DISK_MOUNT_ROOT, file.Name())
		mounted, err := IsMountPoint(repo)
		if err != nil {
			return nil, errors.As(err)
		}
		if !mounted {
			continue
		}
		result[repo] = scanRepo(repo)
	}
	return result, nil
}

func (dpImpl *diskPoolImpl) query(sid string) (SectorState, error) {
	// query from memory cache
	if v, ok := dpImpl.sectors[sid]; ok == true {
		return v, nil
	}

	// load from disks
	diskSectors, err := scanDisk()
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		for repo, sectors := range diskSectors {
			for allocatedSid, _ := range sectors {
				dpImpl.sectors[allocatedSid] = SectorState{
					MountPoint: repo,
				}
			}
		}
	}

	// load from default repo
	sectors := scanRepo(dpImpl.defaultRepo)
	for allocatedSid, _ := range sectors {
		dpImpl.sectors[allocatedSid] = SectorState{
			MountPoint: dpImpl.defaultRepo,
		}
	}

	// query the cache again.
	result, ok := dpImpl.sectors[sid]
	if !ok {
		return SectorState{}, errors.ErrNoData.As(sid)
	}
	return result, nil
}

func (dpImpl *diskPoolImpl) Allocate(sid string) (SectorState, error) {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()

	// query the memory
	state, err := dpImpl.query(sid)
	if err != nil {
		if !errors.ErrNoData.Equal(err) {
			return SectorState{}, errors.As(err)
		}
		// not found in disks, allocate new one.
	} else {
		return state, nil
	}

	// allocate from disk
	diskSectors, err := scanDisk()
	if err != nil {
		return SectorState{}, errors.As(err)
	}
	if len(diskSectors) > 0 {
		dpImpl.hasDisk = true
	}

	minRepo := ""
	minAllocated := math.MaxFloat64
	for repo, sectors := range diskSectors {
		diskInfo, err := DiskUsage(repo, uint64(dpImpl.ssize))
		if err != nil {
			log.Error(errors.As(err))
			continue
		}
		if diskInfo.MaxSector == 0 {
			continue
		}

		allocated := len(sectors)
		// search in memory
		for sector, mState := range dpImpl.sectors {
			if _, ok := sectors[sector]; ok {
				// has allocated
				continue
			}
			if repo != mState.MountPoint {
				// search for the same repo
				continue
			}
			// found in memory but not in disk
			allocated++
		}

		// the sector in transfering is excepted.
		if dpImpl.workerCfg.TransferBuffer > 0 {
			releasedNum := 0
			for sid, _ := range sectors {
				mState, ok := dpImpl.sectors[sid]
				if !ok {
					continue
				}

				if mState.State == database.SECTOR_STATE_PUSH {
					releasedNum++
				}
			}
			for i := dpImpl.workerCfg.TransferBuffer; i > 0; i-- {
				if releasedNum <= 0 {
					break
				}
				allocated--
				releasedNum--
			}
		}

		percent := float64(allocated*100) / float64(diskInfo.MaxSector)
		if percent >= 100 {
			// task is full.
			continue
		}
		if minAllocated > percent {
			minRepo = repo
			minAllocated = percent
		}
	}

	// no disk mounted when start the program, use the default repo.
	if len(minRepo) == 0 && !dpImpl.hasDisk {
		minRepo = dpImpl.defaultRepo
	}

	if len(minRepo) == 0 {
		// no disk for allocation.
		return SectorState{}, errors.New("No disk for allocation").As(sid, diskSectors)
	}

	posState := SectorState{MountPoint: minRepo}
	dpImpl.sectors[sid] = posState
	return posState, nil
}

func (dpImpl *diskPoolImpl) UpdateState(sid string, state int) error {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()
	sector, ok := dpImpl.sectors[sid]
	if !ok {
		return nil
	}
	sector.State = state
	dpImpl.sectors[sid] = sector
	return nil
}

func (dpImpl *diskPoolImpl) Query(sid string) (SectorState, error) {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()
	return dpImpl.query(sid)
}

func (dpImpl *diskPoolImpl) Delete(sid string) error {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()
	delete(dpImpl.sectors, sid)
	return nil
}

func (dpImpl *diskPoolImpl) ShowExt() ([]string, error) {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()

	dpImpl.query("") // just reload the disk data

	var group = map[string][]string{}
	for sid, mnt := range dpImpl.sectors {
		data, ok := group[mnt.MountPoint]
		if !ok {
			data = []string{sid}
		} else {
			data = append(data, sid)
		}
		group[mnt.MountPoint] = data
	}

	r := []string{"\n"}
	for mnt, sids := range group {
		r = append(r, fmt.Sprintf("moint point %s, sector list:", mnt))
		for _, sid := range sids {
			r = append(r, fmt.Sprintf(" %s,", sid))
		}
		r = append(r, "\n")
	}

	return r, nil
}

func (dpImpl *diskPoolImpl) Repos() []string {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()
	repos := []string{
		dpImpl.defaultRepo,
	}

	diskRepos, err := scanDisk()
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		for repo, _ := range diskRepos {
			repos = append(repos, repo)
		}
	}
	return repos
}
