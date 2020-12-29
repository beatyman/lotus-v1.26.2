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
	//DISK_MOUNT_ROOT = "/data/lotus-cache/"
	DISK_MOUNT_ROOT = "/run/user/"
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
}

var dp *SSM
var once sync.Once

func NewDiskPool(ssize abi.SectorSize) (DiskPool, error) {
	var err error
	once.Do(func() {
		fmt.Println("new diskpool instance")
		dp = &SSM{}
		err = dp.init__()
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
	All        uint64 `json:"all"`
	Used       uint64 `json:"used"`
	Free       uint64 `json:"free"`
	MaxSector  uint64 `json:"maxsector"`
	UsedSector uint64 `json:"usedsector"`
}

func sectorallot(disk *DiskStatus) {
	disk.MaxSector = 100
	disk.UsedSector = 0
}

// disk usage of path/disk
func DiskUsage(path string) (disk DiskStatus) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		panic(err)
		return
	}
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
	mnt   string
	mutex sync.Mutex
	ds    DiskStatus
}

type SSM struct {
	mutex  sync.Mutex
	cursor int
	ssize  uint64
	captbl []*diskinfo
	regtbl map[string]string    // sid:moint_point
	maptbl map[string]*[]string // mount_point:sids
}

func (Ssm *SSM) init__() error {
	if _, err := os.Stat(DISK_MOUNT_ROOT); os.IsNotExist(err) {
		return errors.New("please make sure " + DISK_MOUNT_ROOT + "exists")
	}

	dir, err := ioutil.ReadDir(DISK_MOUNT_ROOT)
	if err != nil {
		return err
	}

	Ssm.regtbl = make(map[string]string)
	Ssm.maptbl = make(map[string]*[]string)
	for _, d := range dir {
		if d.IsDir() {
			if IsMountPoint(path.Join(DISK_MOUNT_ROOT, d.Name())) == true {
				Ssm.maptbl[path.Join(DISK_MOUNT_ROOT, d.Name())] = &[]string{}
				Ssm.captbl = append(Ssm.captbl, &diskinfo{mnt: path.Join(DISK_MOUNT_ROOT, d.Name()), ds: DiskUsage(path.Join(DISK_MOUNT_ROOT, d.Name()))})
			}
		}
	}

	for filename, v := range Ssm.maptbl {
		fmt.Println(filename, v)
	}

	fmt.Println("------------mount point capacity and sector allocate:")
	for _, v := range Ssm.captbl {
		fmt.Println(v.mnt, formatFileSize(v.ds.All), v.ds.MaxSector, v.ds.UsedSector)
	}
	if len(Ssm.captbl) > 0 {
		return nil
	}

	return errors.New("No disk is mounted on " + DISK_MOUNT_ROOT)
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

func (Ssm *SSM) Allocate(sid string) (string, error) {
	Ssm.mutex.Lock()
	defer Ssm.mutex.Unlock()
	if v, ok := Ssm.regtbl[sid]; ok == true {
		return v, nil
	}

	len := len(Ssm.captbl)
	for i := 0; i < len; i++ {
		di := Ssm.captbl[Ssm.cursor%len]
		Ssm.cursor++
		di.mutex.Lock()
		defer di.mutex.Unlock()
		if di.ds.UsedSector < di.ds.MaxSector {
			di.ds.UsedSector++
			Ssm.regtbl[sid] = di.mnt

			//
			sids, ok := Ssm.maptbl[di.mnt]
			if !ok {
				return "", errors.New("maptbl not found").As(di.mnt)
			}
			*sids = append(*sids, sid)

			return di.mnt, nil
		}
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
		v.mutex.Lock()
		defer v.mutex.Unlock()
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
