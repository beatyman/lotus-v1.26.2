package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
		return nil, err
	}
	disk := &DiskStatus{}
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	return disk, nil
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
	return false, errors.As(err, mountPoint)
}

// if the mountUri is local file, it would make a link.
func Mount(mountType, mountUri, mountPoint, mountOpts string) error {
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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		if out, err := exec.CommandContext(ctx, "mount", args...).CombinedOutput(); err != nil {
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
		if err := Mount(mountType, mountUri, mountPoint, mountOpt); err != nil {
			if block {
				return errors.As(err, mountUri, mountPoint)
			}
			log.Error(errors.As(err, mountUri, mountPoint))
		}
	}
	return nil
}
func AddStorage(sInfo *StorageInfo) error {
	// set to default value
	//sInfo.SectorSize = 107374182400
	sInfo.UpdateTime = time.Now()
	if err := AddStorageInfo(sInfo); err != nil {
		return errors.As(err, *sInfo)
	}
	if err := Mount(
		sInfo.MountType,
		sInfo.MountSignalUri,
		filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", sInfo.ID)),
		sInfo.MountOpt,
	); err != nil {
		// TODO: rollback insert ?
		return errors.As(err, *sInfo)
	}

	return nil
}

func DisableStorage(id int64) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET disable=1 WHERE id=?", id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func MountStorageByID(id int64) error {
	sInfo, err := GetStorageInfo(id)
	if err != nil {
		return errors.As(err)
	}
	if err := Mount(
		sInfo.MountType,
		sInfo.MountSignalUri,
		filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", id)),
		sInfo.MountOpt,
	); err != nil {
		return errors.As(err, sInfo)
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
