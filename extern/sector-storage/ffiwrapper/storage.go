package ffiwrapper

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

func (sb *Sealer) SectorName(sid abi.SectorID) string {
	return SectorName(sid)
}
func (sb *Sealer) SectorPath(typ, sid string) string {
	return filepath.Join(sb.sectors.RepoPath(), typ, sid)
}

func (sb *Sealer) MakeLink(task *WorkerTask) error {
	sectorName := sb.SectorName(task.SectorID)

	// get path
	newCacheFile := sb.SectorPath("cache", sectorName)
	newSealedFile := sb.SectorPath("sealed", sectorName)

	// make a new link
	ss := task.SectorStorage
	cacheFile := filepath.Join(ss.StorageInfo.MountDir, fmt.Sprintf("%d", ss.StorageInfo.ID), "cache", sectorName)
	if newCacheFile != cacheFile {
		log.Infof("ln -s %s %s", cacheFile, newCacheFile)
		if err := database.Symlink(cacheFile, newCacheFile); err != nil {
			return errors.As(err, ss, sectorName)
		}
	}
	sealedFile := filepath.Join(ss.StorageInfo.MountDir, fmt.Sprintf("%d", ss.StorageInfo.ID), "sealed", sectorName)
	if newSealedFile != sealedFile {
		log.Infof("ln -s %s %s", sealedFile, newSealedFile)
		if err := database.Symlink(sealedFile, newSealedFile); err != nil {
			return errors.As(err, ss, sectorName)
		}
	}
	return nil
}

func (sb *Sealer) AddStorage(ctx context.Context, sInfo *database.StorageInfo) error {
	if err := database.AddStorage(sInfo); err != nil {
		return err
	}
	if sInfo.MaxSize == -1 {
		dir := filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", sInfo.ID))
		diskStatus, err := database.DiskUsage(dir)
		if err != nil {
			return err
		}
		sInfo.MaxSize = int64(diskStatus.All)
		if err = database.UpdateStorageInfo(sInfo); err != nil {
			return err
		}
	}
	if err := database.Mount(
		sInfo.MountType,
		sInfo.MountSignalUri,
		filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", sInfo.ID)),
		sInfo.MountOpt,
	); err != nil {
		return errors.As(err, sInfo)
	}

	return nil
}

func (sb *Sealer) DisableStorage(ctx context.Context, id int64, disable bool) error {
	if err := database.DisableStorage(id, disable); err != nil {
		return errors.As(err)
	}
	if !disable {
		return sb.MountStorage(ctx, id)
	}
	return nil
}

func (sb *Sealer) MountStorage(ctx context.Context, id int64) error {
	sInfo, err := database.GetStorageInfo(id)
	if err != nil {
		return errors.As(err)
	}
	if err := database.Mount(
		sInfo.MountType,
		sInfo.MountSignalUri,
		filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", id)),
		sInfo.MountOpt,
	); err != nil {
		return errors.As(err, sInfo)
	}
	return nil
}

func (sb *Sealer) UMountStorage(ctx context.Context, id int64) error {
	sInfo, err := database.GetStorageInfo(id)
	if err != nil {
		return errors.As(err)
	}
	if _, err := database.Umount(
		filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", id)),
	); err != nil {
		return errors.As(err, sInfo)
	}
	return nil
}

func (sb *Sealer) RelinkStorage(ctx context.Context, storageId int64) error {
	storageInfo, err := database.GetStorageInfo(storageId)
	if err != nil {
		return err
	}
	list, err := database.GetSectorByState(storageId, database.SECTOR_STATE_DONE)
	if err != nil {
		return err
	}
	for _, sectorInfo := range list {
		//获取sectorId
		cacheFile := sb.SectorPath("cache", sectorInfo.ID)
		newName := string(cacheFile)
		oldName := filepath.Join(storageInfo.MountDir, fmt.Sprintf("%d", storageId), "cache", sectorInfo.ID)
		if err := database.Symlink(oldName, newName); err != nil {
			return err
		}
		sealedFile := sb.SectorPath("sealed", sectorInfo.ID)
		newName = string(sealedFile)
		oldName = filepath.Join(storageInfo.MountDir, fmt.Sprintf("%d", storageId), "sealed", sectorInfo.ID)
		if err := database.Symlink(oldName, newName); err != nil {
			return err
		}
	}
	return nil
}

func (sb *Sealer) ReplaceStorage(ctx context.Context, info *database.StorageInfo) error {
	if err := database.Mount(
		info.MountType, info.MountSignalUri,
		filepath.Join(info.MountDir, fmt.Sprintf("%d", info.ID)),
		info.MountOpt); err != nil {
		return errors.As(err)
	}

	// update information
	if err := database.UpdateStorageInfo(info); err != nil {
		return errors.As(err)
	}
	return nil
}

func (sb *Sealer) ScaleStorage(ctx context.Context, id int64, size int64, work int64) error {
	storageInfo, err := database.GetStorageInfo(id)
	if err != nil {
		return err
	}

	// auto detect storage size
	if size == -1 {
		dir := filepath.Join(storageInfo.MountDir, fmt.Sprintf("%d", id))
		diskStatus, err := database.DiskUsage(dir)
		if err != nil {
			return err
		}
		storageInfo.MaxSize = int64(diskStatus.All)
	} else if size > 0 {
		storageInfo.MaxSize = size
	}

	if work > 0 {
		storageInfo.MaxWork = int(work)
	}

	// update information
	if err := database.UpdateStorageInfo(storageInfo); err != nil {
		return errors.As(err, id, size, work)
	}

	return nil
}

func (sb *Sealer) PreStorageNode(sectorId, clientIp string) (*database.StorageInfo, error) {
	_, info, err := database.PrepareStorage(sectorId, clientIp)
	if err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}
func (sb *Sealer) CommitStorageNode(sectorId string) error {
	tx := &database.StorageTx{sectorId}
	return tx.Commit()
}
func (sb *Sealer) CancelStorageNode(sectorId string) error {
	tx := &database.StorageTx{sectorId}
	return tx.Rollback()
}

func (sb *Sealer) ChecksumStorage(sumVer int64) ([]database.StorageInfo, error) {
	return database.ChecksumStorage(sumVer)
}

func (sb *Sealer) StorageStatus(ctx context.Context, id int64, timeout time.Duration) ([]database.StorageStatus, error) {
	repo := sb.sectors.RepoPath()

	checkFn := func(c *database.StorageStatus) error {
		checkPath := filepath.Join(repo, "miner-check.dat")
		if err := ioutil.WriteFile(checkPath, []byte("success"), 0755); err != nil {
			return errors.As(err, checkPath)
		}
		output, err := ioutil.ReadFile(checkPath)
		if err != nil {
			return errors.As(err)
		}
		if string(output) != "success" {
			return errors.New("output not match").As(string(output))
		}
		return nil
	}

	result, err := database.GetStorageCheck(id)
	if err != nil {
		return nil, errors.As(err)
	}

	allLk := sync.Mutex{}
	routines := make(chan bool, 1024) // limit the gorouting to checking the bad, the sectors would be lot.
	done := make(chan bool, len(result))
	for _, c := range result {
		go func(c *database.StorageStatus) {
			// limit the concurrency
			select {
			case routines <- true:
			case <-ctx.Done():
				return
			}
			defer func() {
				<-routines
			}()

			// checking data
			checkDone := make(chan error, 1)
			start := time.Now()
			go func() {
				checkDone <- checkFn(c)
			}()
			var errResult error
			select {
			case <-time.After(timeout):
				// read sector timeout
				errResult = errors.New("sector stat timeout").As(timeout)
			case err := <-checkDone:
				errResult = err
			}
			allLk.Lock()
			c.Used = time.Now().Sub(start)
			c.Err = errResult
			allLk.Unlock()

			// thread end
			done <- true
		}(&c)
	}
	for waits := len(result); waits > 0; waits-- {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, errors.New("user canceled")
		}
	}
	sort.Sort(result)
	return result, nil
}
