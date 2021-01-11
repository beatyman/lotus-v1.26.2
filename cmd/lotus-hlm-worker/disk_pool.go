package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sync"
	"syscall"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gwaylib/errors"
)

const (
	DISK_MOUNT_ROOT = "/data/lotus-cache"
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

	Show() error

	Showext() ([]string, error)
}

var dp *SSM
var once sync.Once

func NewDiskPool(ssize abi.SectorSize) (DiskPool, error) {
	switch ssize {
	case 32 * GB:
	case 512 * MB:
	case 8 * MB:
	case 2 * KB:
	default:
		return nil, errors.New("please check sector size. valid size is: 2KB | 8MB | 512MB | 32GB ")
	}

	var err error
	once.Do(func() {
		fmt.Println("-----------------> new diskpool instance, only create once")
		dp = &SSM{}
		err = dp.init_(uint64(ssize))
		if err == nil {
			err = dp.load_his(uint64(ssize))
		}
	})

	return dp, err
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

func sectorallot(disk *DiskStatus) {
	disk.UsedSector = 0
	if disk.SectorSize == 32*GB {
		disk.MaxSector = disk.All / (500 * GB)
	} else if disk.SectorSize == 512*MB {
		disk.MaxSector = disk.All / (8000 * MB)
	} else if disk.SectorSize == 8*MB {
		disk.MaxSector = disk.All / (125 * MB)
	} else if disk.SectorSize == 2*KB {
		disk.MaxSector = disk.All / (100 * KB)
	} else {
		disk.MaxSector = 0
	}
}

// disk usage of path/disk
func DiskUsage(path string, ssize uint64) (disk DiskStatus) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		panic(err)
		return
	}
	disk.SectorSize = ssize
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	sectorallot(&disk)

	return
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

func exeSysCommand(cmdStr string) ([]byte, error) {
	cmd := exec.Command("sh", "-c", cmdStr)
	return cmd.Output()
}

func IsMountPoint(dir string) bool {
	_, err := exeSysCommand("cat /proc/mounts | grep " + dir)
	if err == nil {
		return true
	}
	fmt.Println(dir, " is not mount point")

	return false
}

type diskinfo struct {
	mnt string
	ds  DiskStatus
}

type SSM struct {
	mutex  sync.Mutex
	ssize  uint64
	captbl []*diskinfo
	regtbl map[string]string    // sid:moint_point
	maptbl map[string]*[]string // mount_point:sids
}

func (Ssm *SSM) init_(ssize uint64) error {
	if _, err := os.Stat(DISK_MOUNT_ROOT); os.IsNotExist(err) {
		return errors.New("please make sure " + DISK_MOUNT_ROOT + "/xxx exists")
	}

	dir, err := ioutil.ReadDir(DISK_MOUNT_ROOT)
	if err != nil {
		return err
	}

	havesubdir := false
	Ssm.regtbl = make(map[string]string)
	Ssm.maptbl = make(map[string]*[]string)
	for _, d := range dir {
		if d.IsDir() {
			havesubdir = true
			if IsMountPoint(path.Join(DISK_MOUNT_ROOT, d.Name())) == true || ssize != 32*GB {
				Ssm.maptbl[path.Join(DISK_MOUNT_ROOT, d.Name())] = &[]string{}
				Ssm.captbl = append(Ssm.captbl, &diskinfo{mnt: path.Join(DISK_MOUNT_ROOT, d.Name()), ds: DiskUsage(path.Join(DISK_MOUNT_ROOT, d.Name()), ssize)})
			}
		}
	}
	if havesubdir == false {
		return errors.New("no subdir as mount point of " + DISK_MOUNT_ROOT + "/")
	}

	flag := false
	fmt.Println("-----------------> Distribution of working cache spaces:")
	fmt.Println("|total capacity\t|sector size\t|max sector\t|used sector\t|mount point|")
	for _, v := range Ssm.captbl {
		fmt.Println("|", formatFileSize(v.ds.All), "\t|", formatFileSize(v.ds.SectorSize), "\t|", v.ds.MaxSector, "\t|", v.ds.UsedSector, "\t|", v.mnt, "\t|")
		if v.ds.MaxSector > 0 {
			flag = true
		}
	}
	fmt.Println("")
	if flag == false {
		return errors.New("mount point " + DISK_MOUNT_ROOT + "/xxx not enough storage space, Cannot store at least one " + formatFileSize(ssize) + " sector")
	}

	if len(Ssm.captbl) > 0 {
		return nil
	}

	return errors.New("No disk is mounted on " + DISK_MOUNT_ROOT)
}

func (Ssm *SSM) load_his(ssize uint64) error {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	if _, err := os.Stat(DISK_MOUNT_ROOT); os.IsNotExist(err) {
		return errors.New("please make sure " + DISK_MOUNT_ROOT + "/xxx exists")
	}

	dir, err := ioutil.ReadDir(DISK_MOUNT_ROOT)
	if err != nil {
		return err
	}

	for _, d := range dir {
		if d.IsDir() {
			if IsMountPoint(path.Join(DISK_MOUNT_ROOT, d.Name())) == true || ssize != 32*GB {
				dirc, err := ioutil.ReadDir(path.Join(DISK_MOUNT_ROOT, d.Name(), SECTOR_CACHE_DIR))
				if err == nil {
					for _, dc := range dirc {
						if dc.IsDir() {
							for _, v := range Ssm.captbl {
								if v.mnt == path.Join(DISK_MOUNT_ROOT, d.Name()) {
									if v.ds.UsedSector < v.ds.MaxSector {
										Ssm.regtbl[dc.Name()] = path.Join(DISK_MOUNT_ROOT, d.Name())
										sids := Ssm.maptbl[v.mnt]
										*sids = append(*sids, dc.Name())
										v.ds.UsedSector++
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func (Ssm *SSM) Allocate(sid string) (string, error) {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	if v, ok := Ssm.regtbl[sid]; ok == true {
		return v, nil
	}

	// 调度规则：按照比利查询可用的，利用率最小的存储盘
	i := -1
	minsector := 100.00
	len := len(Ssm.captbl)
	for j := 0; j < len; j++ {
		di := Ssm.captbl[j%len]
		if (di.ds.UsedSector < di.ds.MaxSector) && (float64(di.ds.UsedSector*100)/float64(di.ds.MaxSector) < minsector) {
			minsector = float64(di.ds.UsedSector*100) / float64(di.ds.MaxSector)
			i = j
		}
	}

	if i == -1 {
		return "", errors.New("worker cache disk resources not available")
	}

	di := Ssm.captbl[i%len]
	if di.ds.UsedSector < di.ds.MaxSector {
		di.ds.UsedSector++
		Ssm.regtbl[sid] = di.mnt

		sids := Ssm.maptbl[di.mnt]
		*sids = append(*sids, sid)

		return di.mnt, nil
	}

	return "", errors.New("No suitable ssd available")
}

func (Ssm *SSM) Query(sid string) (string, error) {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	if v, ok := Ssm.regtbl[sid]; ok == true {
		return v, nil
	}

	return "", errors.New("can't find any ssd cached this sid")
}

func (Ssm *SSM) Delete(sid string) error {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	mnt, ok := Ssm.regtbl[sid]
	if ok == false {
		return errors.New("this sector no mapped to any ssd before")
	}

	for _, v := range Ssm.captbl {
		if v.mnt == mnt {
			if v.ds.UsedSector > 0 {
				v.ds.UsedSector--
			}
		}
	}

	delete(Ssm.regtbl, sid)
	sids := (Ssm.maptbl[mnt])
	len := len(*sids)
	for i := 0; i < len; i++ {
		if sid == (*sids)[i] {
			*sids = append((*sids)[:i], (*sids)[i+1:]...)
			break
		}
	}

	return nil
}

func (Ssm *SSM) Show() error {
	for mnt, sids := range Ssm.maptbl {
		fmt.Println("mnt is:", mnt)
		for _, sid := range *sids {
			fmt.Println("    ", sid)
		}
	}

	return nil
}

func (Ssm *SSM) Showext() ([]string, error) {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	var r []string
	for mnt, sids := range Ssm.maptbl {
		r = append(r, fmt.Sprintf("moint point %s sector list:", mnt))
		for _, sid := range *sids {
			r = append(r, fmt.Sprintf(" %s,", sid))
		}
		r = append(r, "\r\n")
	}

	return r, nil
}

func (Ssm *SSM) Repos() []string {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	repos := []string{}
	for _, disk := range Ssm.captbl {
		repos = append(repos, disk.mnt)
	}
	return repos
}
