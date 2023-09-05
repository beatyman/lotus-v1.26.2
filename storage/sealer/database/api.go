package database

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/storage/sealer/database/lock"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

func Symlink(oldname, newname string) error {
	info, err := os.Lstat(newname)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err, newname)
		}
		// name not exist.
	} else {
		m := info.Mode()
		if m&os.ModeSymlink != os.ModeSymlink {
			return errors.New("target file alread exist").As(newname)
		}
		// clean old data
		if err := os.Remove(newname); err != nil {
			log.Warn(errors.As(err, newname))
			// return errors.As(err, newname)
		}
	}
	if err := os.MkdirAll(filepath.Dir(newname), 0755); err != nil {
		return errors.As(err, newname)
	}
	if err := os.Symlink(oldname, newname); err != nil {
		return errors.As(err, oldname, newname)
	}
	return nil
}

func RemoveSymlink(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err, name)
		}
	} else {
		m := info.Mode()
		if m&os.ModeSymlink != os.ModeSymlink {
			return errors.New("target file alread exist").As(name)
		}
		if err := os.Remove(name); err != nil {
			return errors.As(err, name)
		}
	}
	return nil
}

type DiskStatus struct {
	All  uint64 `json:"all"`
	Used uint64 `json:"used"`
	Free uint64 `json:"free"`
}

// disk usage of path/disk
func DiskUsage(path string) (*DiskStatus, error) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)

	if err != nil {
		return nil, errors.As(err, path)
	}
	disk := &DiskStatus{}
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	return disk, nil
}

var (
	fsLock = "mount.lock"
)

func LockMount(repo string) error {
	_, err := lock.Lock(filepath.Join(repo, fsLock))
	switch {
	case lock.ErrLockedBySelf.Equal(err):
		return nil
	}
	return errors.As(err)
}

func UnlockMount(repo string) error {
	return lock.Unlock(filepath.Join(repo, fsLock))
}

func Umount(mountPoint string) (bool, error) {
	log.Info("storage umount: ", mountPoint)
	// umount
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "umount", "-fl", mountPoint).CombinedOutput()
	if err == nil {
		return true, nil
	}

	if strings.Index(string(out), "not mounted") > -1 {
		// not mounted
		return false, nil
	}
	if strings.Index(string(out), "no mount point") > -1 {
		// no mount point
		return false, nil
	}
	return false, errors.As(err, mountPoint)
}

// if the mountUri is local file, it would make a link.
func Mount(ctx context.Context, mountType, mountUri, mountPoint, mountOpts string) error {
	switch mountType {
	case MOUNT_TYPE_OSS:
		return nil
	case MOUNT_TYPE_FCFS:
		// close for customer protocal
		return nil
	case MOUNT_TYPE_UFILE:
		return nil
	case MOUNT_TYPE_PB:
		return nil
	}

	// umount
	if _, err := Umount(mountPoint); err != nil {
		return errors.As(err, mountPoint)
	}

	// remove link
	info, err := os.Lstat(mountPoint)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err, mountPoint)
		}
		// name not exist.
	} else {
		// clean the mount point
		m := info.Mode()
		if m&os.ModeSymlink == os.ModeSymlink {
			// clean old link
			if err := os.Remove(mountPoint); err != nil {
				return errors.As(err, mountPoint)
			}
		} else if !m.IsDir() {
			return errors.New("file has existed").As(mountPoint)
		}
	}

	switch mountType {
	case MOUNT_TYPE_HLM:
		nfsClient := hlmclient.NewNFSClient(mountUri, mountOpts)
		//nfsClient := hlmclient.NewNFSClient(mountUri, mountOpts)
		if err := nfsClient.Mount(ctx, mountPoint); err != nil {
			return errors.As(err, mountPoint)
		}
		return nil
	case "":
		if err := os.MkdirAll(filepath.Dir(mountPoint), 0755); err != nil {
			return errors.As(err, mountUri, mountPoint)
		}
		if err := os.Symlink(mountUri, mountPoint); err != nil {
			return errors.As(err, mountUri, mountPoint)
		}
	default:
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return errors.As(err)
		}

		args := []string{
			"-t", mountType, mountUri, mountPoint,
		}
		opts := strings.Split(mountOpts, " ")
		for _, opt := range opts {
			o := strings.TrimSpace(opt)
			if len(o) > 0 {
				args = append(args, o)
			}
		}
		log.Info("storage mount", args)
		timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if out, err := exec.CommandContext(timeoutCtx, "mount", args...).CombinedOutput(); err != nil {
			cancel()
			return errors.As(err, string(out), args)
		}
		cancel()
	}
	return nil
}

func MountAllStorage(block bool) error {
	db := GetDB()
	rows, err := db.Query("SELECT id, mount_type, mount_signal_uri, mount_dir, mount_opt FROM storage_info WHERE disable=0")
	if err != nil {
		return errors.As(err)
	}
	defer rows.Close()
	var id int64
	var mountType, mountUri, mountDir, mountOpt string
	for rows.Next() {
		if err := rows.Scan(&id, &mountType, &mountUri, &mountDir, &mountOpt); err != nil {
			return errors.As(err)
		}
		mountPoint := filepath.Join(mountDir, fmt.Sprintf("%d", id))
		log.Infof("mount: mountType: %+v, mountUri: %+v, mountPoint: %+v", mountType, mountUri, mountPoint)
		if err := Mount(context.TODO(), mountType, mountUri, mountPoint, mountOpt); err != nil {
			if block {
				return errors.As(err, mountUri, mountPoint)
			}
			log.Error(errors.As(err, mountUri, mountPoint))
		}
	}
	return nil
}

// if the mountUri is local file, it would make a link.
func MountPostWorker(ctx context.Context, mountType, mountUri, mountPoint, mountOpts string) error {
	switch mountType {
	case MOUNT_TYPE_PB:
		return nil
	case MOUNT_TYPE_OSS:
		return nil
	case MOUNT_TYPE_UFILE:
		return nil
	case MOUNT_TYPE_FCFS:
		// close for customer protocal
		//判断服务是否启动
		serviceStart, err := CheckProRunning("kodo-fcfs")
		if err != nil {
			return errors.As(err, mountPoint)
		}
		//如果服务未启动，启动服务
		if !serviceStart {
			_, err = RunCommand("systemctl start kodo-fcfs.service ")
			if err != nil {
				return errors.As(err, " start service fault ", mountPoint)
			}
		}
		//判断根目录存不存在，不存在直接报错
		_, err = os.Stat(mountUri)
		if err != nil {
			if os.IsNotExist(err) {
				os.MkdirAll(mountUri,0776)
			}else{
				return errors.As(err,  mountUri)
			}
		}
		//判断目录是否存在
		_, err = os.Stat(mountPoint)
		isLink := false
		if err == nil {

			fileInfo, err := os.Lstat(mountPoint)
			if err != nil {
				return errors.As(err, " os.Lstat ", mountPoint)
			}
			if fileInfo.Mode()&os.ModeSymlink != 0 {
				log.Info("===========目录是软连接不做操作", mountPoint)
				//fmt.Println("目录是软连接，删除", mountPoint)
				//err = os.RemoveAll(mountPoint)
				//if err != nil {
				//	fmt.Println("删除失败！")
				//	return errors.As(err, " RemoveAll ", mountPoint)
				//}
				//isLink = true
			} else {
				//判断里面是否有文件，如果有文件，则不删除， 没有则删除
				//files, err := os.Open(mountPoint) //open the directory to read files in the directory
				//if err != nil {
				//	fmt.Println("error opening directory:", err) //print error if directory is not opened
				//	return errors.As(err, " Open ", mountPoint)
				//}
				//defer files.Close() //close the directory opened

				//fileInfos, err := files.Readdir(-1) //read the files from the directory
				//if err != nil {
				//	fmt.Println("error reading directory:", err) //if directory is not read properly print error message
				//	return errors.As(err, " Readdir ", mountPoint)
				//}
				//if len(fileInfos) > 0 {
				//	return nil
				//}
				//err = os.RemoveAll(mountPoint)
				//if err != nil {
				//	fmt.Println("删除失败！")
				//	return errors.As(err, " RemoveAll ", mountPoint)
				//}
				//isLink = true
			}
		} else {
			//目录不存在，创建软连接
			isLink = true
		}
		if isLink {
			//检查上级目录存不存在
			_, err := os.Stat(mountPoint[0:strings.LastIndex(mountPoint, "/")])
			if err != nil {
				os.MkdirAll(mountPoint[0:strings.LastIndex(mountPoint, "/")], 0776)
			}
			err = os.Symlink(mountUri, mountPoint)
			if err != nil {
				return errors.As(err, " Symlink ", mountPoint)
			}
		}
		return nil
	}

	// umount
	if _, err := Umount(mountPoint); err != nil {
		return errors.As(err, mountPoint)
	}

	// remove link
	info, err := os.Lstat(mountPoint)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err, mountPoint)
		}
		// name not exist.
	} else {
		// clean the mount point
		m := info.Mode()
		if m&os.ModeSymlink == os.ModeSymlink {
			// clean old link
			if err := os.Remove(mountPoint); err != nil {
				return errors.As(err, mountPoint)
			}
		} else if !m.IsDir() {
			return errors.New("file has existed").As(mountPoint)
		}
	}

	switch mountType {
	case MOUNT_TYPE_HLM:
		nfsClient := hlmclient.NewNFSClient(mountUri, mountOpts)
		//nfsClient := hlmclient.NewNFSClient(mountUri, mountOpts)
		if err := nfsClient.Mount(ctx, mountPoint); err != nil {
			return errors.As(err, mountPoint)
		}
		return nil
	case "":
		if err := os.MkdirAll(filepath.Dir(mountPoint), 0755); err != nil {
			return errors.As(err, mountUri, mountPoint)
		}
		if err := os.Symlink(mountUri, mountPoint); err != nil {
			return errors.As(err, mountUri, mountPoint)
		}
	default:
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return errors.As(err)
		}

		args := []string{
			"-t", mountType, mountUri, mountPoint,
		}
		opts := strings.Split(mountOpts, " ")
		for _, opt := range opts {
			o := strings.TrimSpace(opt)
			if len(o) > 0 {
				args = append(args, o)
			}
		}
		log.Info("storage mount", args)
		timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if out, err := exec.CommandContext(timeoutCtx, "mount", args...).CombinedOutput(); err != nil {
			cancel()
			return errors.As(err, string(out), args)
		}
		cancel()
	}
	return nil
}

// gc concurrency worker on the storage.
func GetTimeoutTask(invalidTime time.Time) ([]SectorInfo, error) {
	db := GetDB()
	result := []SectorInfo{}
	if err := database.QueryStructs(db, &result, "SELECT * FROM sector_info WHERE state<?", 200); err != nil {
		return nil, errors.As(err)
	}

	dropTasks := []SectorInfo{}
	for _, info := range result {
		if info.CreateTime.Before(invalidTime) {
			dropTasks = append(dropTasks, info)
			continue
		}
	}
	return dropTasks, nil
}

func IsExeRuning(strKey string, strExeName string) bool {
	buf := bytes.Buffer{}
	cmd := exec.Command("wmic", "process", "get", "name,executablepath")
	cmd.Stdout = &buf
	cmd.Run()

	cmd2 := exec.Command("findstr", strKey)
	cmd2.Stdin = &buf
	data, err := cmd2.CombinedOutput()
	if err != nil && err.Error() != "exit status 1" {
		//XBLog.LogF("ServerMonitor", "IsExeRuning CombinedOutput error, err:%s", err.Error())
		return false
	}

	strData := string(data)
	if strings.Contains(strData, strExeName) {
		return true
	} else {
		return false
	}
}

func runInWindows(cmd string) (string, error) {
	result, err := exec.Command("cmd", "/c", cmd).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result)), err
}

func RunCommand(cmd string) (string, error) {
	if runtime.GOOS == "windows" {
		return runInWindows(cmd)
	} else {
		return runInLinux(cmd)
	}
}

func Link(cmd string) (string, error) {
	out, err := exec.Command("ll", cmd).Output()
	if err != nil {
		fmt.Println("执行命令出错：", err)
		return "", err
	}

	fmt.Println(string(out))
	return string(out), nil
}

func runInLinux(cmd string) (string, error) {
	fmt.Println("Running Linux cmd:" + cmd)
	result, err := exec.Command("/bin/sh", "-c", cmd).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result)), err
}

// 根据进程名判断进程是否运行
func CheckProRunning(serverName string) (bool, error) {
	a := `ps ux | awk '/` + serverName + `/ && !/awk/ {print $2}'`
	pid, err := RunCommand(a)
	if err != nil {
		return false, err
	}
	return pid != "", nil
}

// 根据进程名称获取进程ID
func GetPid(serverName string) (string, error) {
	a := `ps ux | awk '/` + serverName + `/ && !/awk/ {print $2}'`
	pid, err := RunCommand(a)
	return pid, err
}
