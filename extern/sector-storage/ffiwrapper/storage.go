package ffiwrapper

import (
	"context"
	"fmt"
	"path/filepath"
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

func (sb *Sealer) AddStorage(ctx context.Context, sInfo database.StorageInfo) error {
	if err := database.AddStorage(&sInfo); err != nil {
		return err
	}
	if sInfo.MaxSize == -1 {
		dir := filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", sInfo.ID))
		diskStatus, err := database.DiskUsage(dir)
		if err != nil {
			return err
		}
		sInfo.MaxSize = int64(diskStatus.All)
		if err = database.UpdateStorageInfo(&sInfo); err != nil {
			return err
		}
	}

	return nil
}

func (sb *Sealer) DisableStorage(ctx context.Context, id int64) error {
	return database.DisableStorage(id)
}

func (sb *Sealer) MountStorage(ctx context.Context, id int64) error {
	return database.MountStorageByID(id)
}

func (sb *Sealer) UMountStorage(ctx context.Context, id int64) error {
	// close this function
	// https://git.grandhelmsman.com/filecoin-project/requirement/issues/60
	return nil
}
func (sb *Sealer) RelinkStorage(ctx context.Context, id int64) error {
	return sb.relinkStorageByStorageId(id)
}
func (sb *Sealer) ReplaceStorage(ctx context.Context, id int64, signalUri, transfUri, mountType, mountOpt string) error {
	storageInfo, err := database.GetStorageInfo(id)
	if err != nil {
		return err
	}
	storageInfo.MountSignalUri = signalUri
	storageInfo.MountTransfUri = transfUri
	if len(mountType) > 0 {
		storageInfo.MountType = mountType
	}
	if len(mountOpt) > 0 {
		storageInfo.MountOpt = mountOpt
	}
	storageInfo.Version = time.Now().UnixNano() //  upgrade the data version
	if err := database.Mount(mountType, signalUri,
		filepath.Join(storageInfo.MountDir, fmt.Sprintf("%d", storageInfo.ID)),
		mountOpt); err != nil {
		return errors.As(err)
	}

	// update information
	if err := database.UpdateStorageInfo(storageInfo); err != nil {
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
func (sb *Sealer) relinkStorageByStorageId(storageId int64) error {
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

func (sb *Sealer) ChecksumStorage(sumVer int64) (database.StorageList, error) {
	return database.ChecksumStorage(sumVer)
}
