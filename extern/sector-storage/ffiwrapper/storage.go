package ffiwrapper

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

func (sb *Sealer) SectorPath(typ, sid string) string {
	return filepath.Join(sb.sectors.RepoPath(), typ, sid)
}

func (sb *Sealer) MakeLink(task *WorkerTask) error {
	sectorName := sectorName(task.SectorID)

	// get path
	newCacheFile := sb.SectorPath("cache", sectorName)
	newSealedFile := sb.SectorPath("sealed", sectorName)
	newUnsealedFile := sb.SectorPath("unsealed", sectorName)

	// make a new link
	ss := task.SectorStorage
	sealedStorage := ss.SealedStorage
	unsealedStorage := ss.UnsealedStorage
	cacheFile := filepath.Join(sealedStorage.MountDir, fmt.Sprintf("%d", sealedStorage.ID), "cache", sectorName)
	if newCacheFile != cacheFile {
		log.Infof("ln -s %s %s", cacheFile, newCacheFile)
		if err := database.Symlink(cacheFile, newCacheFile); err != nil {
			return errors.As(err, ss, sectorName)
		}
	}
	sealedFile := filepath.Join(sealedStorage.MountDir, fmt.Sprintf("%d", sealedStorage.ID), "sealed", sectorName)
	if newSealedFile != sealedFile {
		log.Infof("ln -s %s %s", sealedFile, newSealedFile)
		if err := database.Symlink(sealedFile, newSealedFile); err != nil {
			return errors.As(err, ss, sectorName)
		}
	}
	unsealedFile := filepath.Join(unsealedStorage.MountDir, fmt.Sprintf("%d", unsealedStorage.ID), "unsealed", sectorName)
	if _, err := os.Stat(unsealedFile); err == nil && newUnsealedFile != unsealedFile {
		log.Infof("ln -s %s %s", unsealedFile, newUnsealedFile)
		if err := database.Symlink(unsealedFile, newUnsealedFile); err != nil {
			return errors.As(err, ss, sectorName)
		}
	}
	return nil
}

func (sb *Sealer) AddStorage(ctx context.Context, sInfo *database.StorageInfo) error {
	id, err := database.AddStorageInfo(sInfo)
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

	if sInfo.MaxSize == -1 {
		dir := filepath.Join(sInfo.MountDir, fmt.Sprintf("%d", id))
		diskStatus, err := database.DiskUsage(dir)
		if err != nil {
			return errors.As(err)
		}
		sInfo.MaxSize = int64(diskStatus.All)
		if err = database.UpdateStorageInfo(sInfo); err != nil {
			return errors.As(err)
		}
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
	storageInfos := []database.StorageInfo{}
	if id > 0 {
		sInfo, err := database.GetStorageInfo(id)
		if err != nil {
			return errors.As(err)
		}
		storageInfos = append(storageInfos, *sInfo)
	} else {
		infos, err := database.GetAllStorageInfo()
		if err != nil {
			return errors.As(err)
		}
		storageInfos = infos
	}
	for _, info := range storageInfos {
		if err := database.Mount(
			info.MountType,
			info.MountSignalUri,
			filepath.Join(info.MountDir, fmt.Sprintf("%d", info.ID)),
			info.MountOpt,
		); err != nil {
			return errors.As(err, info)
		}
	}
	return nil
}

func (sb *Sealer) UMountStorage(ctx context.Context, id int64) error {
	storageInfos := []database.StorageInfo{}
	if id > 0 {
		sInfo, err := database.GetStorageInfo(id)
		if err != nil {
			return errors.As(err)
		}
		storageInfos = append(storageInfos, *sInfo)
	} else {
		infos, err := database.GetAllStorageInfo()
		if err != nil {
			return errors.As(err)
		}
		storageInfos = infos
	}
	for _, info := range storageInfos {
		if _, err := database.Umount(
			filepath.Join(info.MountDir, fmt.Sprintf("%d", info.ID)),
		); err != nil {
			return errors.As(err, info)
		}
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

	// update the storage max version when done a replace operation
	info.Version = time.Now().UnixNano()

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

func (sb *Sealer) PreStorageNode(sectorId, clientIp string, kind int) (*database.StorageInfo, error) {
	_, info, err := database.PrepareStorage(sectorId, clientIp, kind)
	if err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}
func (sb *Sealer) CommitStorageNode(sectorId string, kind int) error {
	tx := &database.StorageTx{SectorId: sectorId, Kind: kind}
	return tx.Commit()
}
func (sb *Sealer) CancelStorageNode(sectorId string, kind int) error {
	tx := &database.StorageTx{SectorId: sectorId, Kind: kind}
	return tx.Rollback()
}

func (sb *Sealer) ChecksumStorage(sumVer int64) ([]database.StorageInfo, error) {
	return database.ChecksumStorage(sumVer)
}

func (sb *Sealer) StorageStatus(ctx context.Context, id int64, origin bool, timeout time.Duration) ([]database.StorageStatus, error) {
	httpsClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	checkFn := func(c *database.StorageStatus) error {
		mountPath := filepath.Join(c.MountDir, fmt.Sprintf("%d", c.StorageId))

		checkPath := filepath.Join(mountPath, "miner-check.dat")
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

		if !origin || c.Kind != 0 {
			return nil
		}

		// TODO: set the url to database.
		// checkt the storage status
		uri := strings.Split(c.MountUri, ":")
		if len(uri) < 1 {
			return errors.New("error mount uri format")
		}
		resp, err := httpsClient.Get("https://" + uri[0] + ":1330/check")
		if err != nil {
			return errors.As(err)
		}
		defer resp.Body.Close()
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return errors.As(err)
		}
		if strings.Contains(string(data), "all pools are healthy") {
			return nil
		}
		if len(data) > 0 {
			return errors.New(string(data))
		}
		return errors.New("zpool error").As(resp.StatusCode, string(data))
	}

	result, err := database.GetStorageCheck(id)
	if err != nil {
		return nil, errors.As(err)
	}

	allLk := sync.Mutex{}
	routines := make(chan bool, 1024) // limit the gorouting to checking the bad, the sectors would be lot.
	done := make(chan bool, len(result))
	checked := map[string]bool{}
	for i, s := range result {
		// no check for disable
		if s.Disable {
			done <- true
			continue
		}

		// when it's the same uri, just do once.
		if _, ok := checked[s.MountUri]; ok {
			done <- true
			continue
		}
		checked[s.MountUri] = true

		go func(c *database.StorageStatus) {
			// limit the concurrency
			routines <- true
			defer func() {
				<-routines
			}()

			start := time.Now()
			// checking data
			checkDone := make(chan error, 1)
			go func() {
				checkDone <- checkFn(c)
			}()

			var errResult error
			select {
			case <-ctx.Done():
				// user canceled
				errResult = errors.New("ctx canceled").As(c.StorageId)
			case <-time.After(timeout):
				// read sector timeout
				errResult = errors.New("sector stat timeout").As(timeout, c.StorageId)
			case err := <-checkDone:
				errResult = err
			}
			allLk.Lock()
			c.Used = time.Now().Sub(start)
			if errResult != nil {
				c.Err = errResult.Error()
			}
			allLk.Unlock()

			// thread end
			done <- true
		}(&result[i])
	}
	for waits := len(result); waits > 0; waits-- {
		<-done
	}
	sort.Sort(result)
	return result, nil
}
