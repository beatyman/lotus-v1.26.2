package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/filecoin-project/go-state-types/abi"
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
	Allocate(sid string) (string, error)

	// query sid's cache directory with sector id
	Query(sid string) (string, error)

	// delete disk data with sector id
	Delete(sid string) error

	ShowExt() ([]string, error)
}

type diskinfo struct {
	mnt string
	ds  DiskStatus
}

type diskPoolImpl struct {
	mutex       sync.Mutex
	ssize       abi.SectorSize
	defaultRepo string

	sectors map[string]string // sid:moint_point
}

// defaultRepo -- if disk pool not found, use the old repo for work.
func NewDiskPool(ssize abi.SectorSize, defaultRepo string) DiskPool {
	return &diskPoolImpl{
		ssize:       ssize,
		defaultRepo: defaultRepo,
		sectors:     map[string]string{},
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
	case 32 * GB:
		return total / (500 * GB)
	case 512 * MB:
		return total / (8000 * MB)
	case 8 * MB:
		return total / (125 * KB)
	case 2 * KB:
		return total / (100 * KB)
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

func IsMountPoint(dir string) bool {
	ctx, cancel := context.WithTimeout(context.TODO(), 3e9)
	defer cancel()
	if err := exec.CommandContext(ctx, "mountpoint", "-q", dir).Run(); err != nil {
		log.Info(errors.As(err, dir))
		return false
	}
	return true
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
		if !IsMountPoint(repo) {
			continue
		}
		result[repo] = scanRepo(repo)
	}
	return result, nil
}

func (dpImpl *diskPoolImpl) query(sid string) (string, error) {
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
				dpImpl.sectors[allocatedSid] = repo
			}
		}
	}

	// load from default repo
	sectors := scanRepo(dpImpl.defaultRepo)
	for allocatedSid, _ := range sectors {
		dpImpl.sectors[allocatedSid] = dpImpl.defaultRepo
	}

	// query the cache again.
	result, ok := dpImpl.sectors[sid]
	if !ok {
		return "", errors.ErrNoData.As(sid)
	}
	return result, nil
}

func (dpImpl *diskPoolImpl) Allocate(sid string) (string, error) {
	dpImpl.mutex.Lock()
	defer dpImpl.mutex.Unlock()

	repo, err := dpImpl.query(sid)
	if err != nil {
		if !errors.ErrNoData.Equal(err) {
			return "", errors.As(err)
		}
		// not found in disks, allocate new one.
	} else {
		return repo, nil
	}

	// allocate from disk
	diskSectors, err := scanDisk()
	if err != nil {
		return "", errors.As(err)
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
		for sector, memRepo := range dpImpl.sectors {
			if _, ok := sectors[sector]; ok {
				// has allocated
				continue
			}
			if repo != memRepo {
				// search for the same repo
				continue
			}
			// found in memory but not in disk
			allocated++
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

	// no disk mounted, use the default repo.
	if len(minRepo) == 0 && len(diskSectors) == 0 {
		minRepo = dpImpl.defaultRepo
	}

	if len(minRepo) == 0 {
		// no disk for allocation.
		return "", errors.ErrNoData.As(sid, diskSectors)
	}

	dpImpl.sectors[sid] = minRepo
	return minRepo, nil
}

func (dpImpl *diskPoolImpl) Query(sid string) (string, error) {
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
		data, ok := group[mnt]
		if !ok {
			data = []string{sid}
		} else {
			data = append(data, sid)
		}
		group[mnt] = data
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
