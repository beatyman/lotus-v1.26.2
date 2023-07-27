package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/gwaylib/errors"
)

var PushRepoPath = "/data/lotus-push/tmp"

// remove cache of the sector
func (w *worker) RemoveRepoSector(ctx context.Context, repo, sid string) error {
	return w.removeRepoSector(ctx, repo, sid)
}

func (w *worker) removeRepoSector(ctx context.Context, repo, sid string) error {
	log.Infof("Remove cache:%s,%s", repo, sid)
	if err := os.Remove(filepath.Join(repo, "sealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(repo, "cache", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.Remove(filepath.Join(repo, "unsealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.Remove(filepath.Join(repo, "update", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(repo, "update-cache", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(repo, "fstmp", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := w.diskPool.Delete(sid); err != nil {
		log.Error(errors.As(err))
	}
	ffiwrapper.FreeTaskPid(sid)
	return nil
}

// auto clean cache of the unusing sector.
func (w *worker) GcRepoSectors(ctx context.Context) error {
	repos := w.diskPool.Repos()

	for _, repo := range repos {
		if err := w.gcRepo(ctx, repo, "sealed"); err != nil {
			return errors.As(err)
		}
		if err := w.gcRepo(ctx, repo, "cache"); err != nil {
			return errors.As(err)
		}
		if err := w.gcRepo(ctx, repo, "unsealed"); err != nil {
			return errors.As(err)
		}
		if err := w.gcRepo(ctx, repo, "fstmp"); err != nil {
			return errors.As(err)
		}
		//delete(w.sealers, repo)
	}
	return nil
}

func (w *worker) gcRepo(ctx context.Context, repo, typ string) error {
	dir := filepath.Join(repo, typ)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		fileNames := []string{}
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		// no file to clean
		if len(fileNames) == 0 {
			return nil
		}

		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		ws, err := api.RetryWorkerWorkingById(ctx, fileNames)
		if err != nil {
			return errors.As(err, fileNames)
		}
	sealedLoop:
		for _, f := range files {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue sealedLoop
				}
			}
			if err := w.removeRepoSector(ctx, repo, f.Name()); err != nil {
				return errors.As(err, w.workerCfg.IP, repo, typ, f.Name())
			}
		}
	}
	return nil
}

func (w *worker) pushSealed(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()

	log.Infof("pushSealed:%+v", sid)
	defer log.Infof("pushSealed exit:%+v", sid)

	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	ss, err := api.RetryPreStorageNode(ctx, sid, w.workerCfg.IP, database.STORAGE_KIND_SEALED)
	if err != nil {
		return errors.As(err)
	}
	log.Info("====================pushSealed===================", w.sealedRepo)
	switch ss.MountType {
	case database.MOUNT_TYPE_HLM:
		tmpAuth, err := api.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		// send the sealed
		sealedFromPath := workerSB.SectorPath("sealed", sid)
		if err := fc.Upload(ctx, sealedFromPath, filepath.Join("sealed", sid)); err != nil {
			return errors.As(err)
		}
		// send the cache
		cacheFromPath := workerSB.SectorPath("cache", sid)
		if err := fc.Upload(ctx, cacheFromPath, filepath.Join("cache", sid)); err != nil {
			return errors.As(err)
		}
		if err := api.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	case database.MOUNT_TYPE_OSS:
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		// send the sealed
		sealedFromPath := workerSB.SectorPath("sealed", sid)
		sealedToPath := filepath.Join(mountDir, "sealed")
		if err := w.upload(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("cache", sid)
		cacheToPath := filepath.Join(mountDir, "cache", sid)
		if err := w.upload(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
	case database.MOUNT_TYPE_CUSTOM:
		mountDir := filepath.Join(PushRepoPath, sid)
		//if err := database.MountPostWorker(
		//	ctx,
		//	ss.MountType,
		//	ss.MountSignalUri,
		//	mountDir,
		//	ss.MountOpt,
		//); err != nil {
		//	return errors.As(err, "======MountPostWorker fault", mountDir)
		//}
		// send the sealed
		sealedFromPath := workerSB.SectorPath("sealed", sid)
		sealedToPath := filepath.Join(mountDir, "sealed")
		if err := os.MkdirAll(sealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		//// push do a deep checksum
		//hashKind := _HASH_KIND_NONE
		//if w.needMd5Sum {
		//	hashKind = _HASH_KIND_DEEP
		//}
		if err := w.rsync(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("cache", sid)
		cacheToPath := filepath.Join(mountDir, "cache", sid)
		if err := os.MkdirAll(cacheToPath, 0755); err != nil {
			return errors.As(err)
		}
		// push do a deep checksum
		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
		//成功之后删除软连接
		//log.Info("push seal remove Link ====", mountDir)
		//os.RemoveAll(mountDir)

	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return errors.As(err)
		}

		// send the sealed
		sealedFromPath := workerSB.SectorPath("sealed", sid)
		sealedToPath := filepath.Join(mountDir, "sealed")
		if err := os.MkdirAll(sealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("cache", sid)
		cacheToPath := filepath.Join(mountDir, "cache", sid)
		if err := os.MkdirAll(cacheToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return errors.As(err)
		}
	}

	if err := api.RetryCommitStorageNode(ctx, sid, database.STORAGE_KIND_SEALED); err != nil {
		return errors.As(err)
	}
	return nil
}
func (w *worker) pushUnsealed(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()
	log.Infof("pushUnsealed:%+v", sid)
	defer log.Infof("pushUnsealed exit:%+v", sid)

	ss := task.SectorStorage.UnsealedStorage
	if ss.ID == 0 {
		// no unsealed storage to mount
		return nil
	}

	switch ss.MountType {
	case database.MOUNT_TYPE_HLM:
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		tmpAuth, err := api.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		fileFromPath := workerSB.SectorPath("unsealed", sid)
		// make truncate because the file changed after resealed.
		if err := fc.Truncate(ctx, filepath.Join("unsealed", sid), 0); err != nil {
			return errors.As(err)
		}
		if err := fc.Upload(ctx, fileFromPath, filepath.Join("unsealed", sid)); err != nil {
			return errors.As(err)
		}
		if err := api.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	case database.MOUNT_TYPE_OSS:
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		unsealedFromPath := workerSB.SectorPath("unsealed", sid)
		unsealedToPath := filepath.Join(mountDir, "unsealed", sid)
		if err := w.upload(ctx, unsealedFromPath, unsealedToPath); err != nil {
			return errors.As(err)
		}
	case database.MOUNT_TYPE_CUSTOM:
		mountDir := filepath.Join(PushRepoPath, sid)
		//if err := database.MountPostWorker(
		//	ctx,
		//	ss.MountType,
		//	ss.MountSignalUri,
		//	mountDir,
		//	ss.MountOpt,
		//); err != nil {
		//	return errors.As(err, "======MountPostWorker fault", mountDir)
		//}
		// send the sealed
		unsealedFromPath := workerSB.SectorPath("unsealed", sid)
		unsealedToPath := filepath.Join(mountDir, "unsealed")
		if err := os.MkdirAll(unsealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, unsealedFromPath, filepath.Join(unsealedToPath, sid)); err != nil {
			return errors.As(err)
		}
		//成功之后删除软连接
		//log.Info("push unseal remove Link ====", mountDir)
		//os.RemoveAll(mountDir)
	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return errors.As(err)
		}

		unsealedFromPath := workerSB.SectorPath("unsealed", sid)
		unsealedToPath := filepath.Join(mountDir, "unsealed")
		if err := os.MkdirAll(unsealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, unsealedFromPath, filepath.Join(unsealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

func (w *worker) fetchUnseal(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()
	log.Infof("fetchUnseal:%+v", sid)
	defer log.Infof("fetchUnseal exit:%+v", sid)

	ss := task.SectorStorage.UnsealedStorage
	if ss.ID == 0 {
		log.Infof("the unseal storage not found, ignore:%s", sid)
		return nil
	}

	switch ss.MountType {
	case database.MOUNT_TYPE_HLM:
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		tmpAuth, err := api.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		toFilePath := workerSB.SectorPath("unsealed", sid)
		if err := fc.Download(ctx, toFilePath, filepath.Join("unsealed", sid)); err != nil {
			if errors.As(io.EOF).Equal(err) {
				// remove local file
				if err := os.Remove(toFilePath); err != nil {
					log.Error(errors.As(err))
				}
			}
			if !os.IsNotExist(err) {
				return errors.As(err)
			}
			// it's ok if the unsealed not exist.
		}
		if err := api.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	case database.MOUNT_TYPE_OSS:
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		unsealedFromPath := filepath.Join(mountDir, "unsealed", sid)
		unsealedToPath := workerSB.SectorPath("unsealed", sid)
		if err := w.download(ctx, unsealedFromPath, unsealedToPath); err != nil {
			return errors.As(err)
		}
	case database.MOUNT_TYPE_CUSTOM:
		mountDir := filepath.Join(PushRepoPath, sid)
		//if err := database.MountPostWorker(
		//	ctx,
		//	ss.MountType,
		//	ss.MountSignalUri,
		//	mountDir,
		//	ss.MountOpt,
		//); err != nil {
		//	return errors.As(err, "======MountPostWorker fault", mountDir)
		//}
		// send the sealed
		unsealedToPath := workerSB.SectorPath("unsealed", sid)
		unsealedFromPath := filepath.Join(mountDir, "unsealed", sid)
		if _, err := os.Stat(unsealedFromPath); err != nil {
			log.Warn("%+v", err.Error())
			//os.RemoveAll(mountDir)
			return nil
		}
		if err := w.rsync(ctx, unsealedFromPath, unsealedToPath); err != nil {
			return errors.As(err)
		}
		//成功之后删除软连接
		//log.Info("fetchUnseal remove Link ====", mountDir)
		//os.RemoveAll(mountDir)
	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return errors.As(err)
		}

		// fetch the unsealed file
		unsealedFromPath := filepath.Join(mountDir, "unsealed", sid)
		unsealedToPath := workerSB.SectorPath("unsealed", sid)
		if err := w.rsync(ctx, unsealedFromPath, unsealedToPath); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return errors.As(err)
			}
			// no data to fetch.
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return errors.As(err)
		}
	}
	return nil
}
func (w *worker) fetchSealed(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()
	log.Infof("fetchSealed:%+v", sid)
	defer log.Infof("fetchSealed exit:%+v", sid)

	ss := task.SectorStorage.SealedStorage
	if ss.ID == 0 {
		return errors.New("the seal storage not found").As(sid)
	}

	switch ss.MountType {
	case database.MOUNT_TYPE_HLM:
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		tmpAuth, err := api.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		sealedToPath := workerSB.SectorPath("sealed", sid)
		if err := fc.Download(ctx, sealedToPath, filepath.Join("sealed", sid)); err != nil {
			return errors.As(err)
		}
		cacheToPath := workerSB.SectorPath("cache", sid)
		if err := fc.Download(ctx, cacheToPath, filepath.Join("cache", sid)); err != nil {
			return errors.As(err)
		}
		if err := api.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	case database.MOUNT_TYPE_OSS:
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		// fetch the unsealed file
		sealedFromPath := filepath.Join(mountDir, "sealed", sid)
		sealedToPath := workerSB.SectorPath("sealed", sid)
		if err := w.download(ctx, sealedFromPath, sealedToPath); err != nil {
			return errors.As(err)
		}

		// fetch cache file
		cacheFromPath := filepath.Join(mountDir, "cache", sid)
		cacheToPath := workerSB.SectorPath("cache", sid)
		if err := w.download(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
	case database.MOUNT_TYPE_CUSTOM:
		mountDir := filepath.Join(PushRepoPath, sid)
		//if err := database.MountPostWorker(
		//	ctx,
		//	ss.MountType,
		//	ss.MountSignalUri,
		//	mountDir,
		//	ss.MountOpt,
		//); err != nil {
		//	return errors.As(err, "======MountPostWorker fault", mountDir)
		//}
		// send the sealed
		sealedToPath := workerSB.SectorPath("sealed", sid)
		sealedFromPath := filepath.Join(mountDir, "sealed", sid)

		if err := w.rsync(ctx, sealedFromPath, sealedToPath); err != nil {
			return errors.As(err)
		}

		cacheToPath := workerSB.SectorPath("cache", sid)
		cacheFromPath := filepath.Join(mountDir, "cache", sid)

		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
		log.Info("sealedToPath = ", sealedToPath, " ====== cacheToPath", cacheToPath)
		//成功之后删除软连接
		//log.Info("fetchSeal remove Link ====", mountDir)
		//os.RemoveAll(mountDir)
	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return errors.As(err)
		}

		// fetch the unsealed file
		sealedFromPath := filepath.Join(mountDir, "sealed", sid)
		sealedToPath := workerSB.SectorPath("sealed", sid)
		if err := w.rsync(ctx, sealedFromPath, sealedToPath); err != nil {
			return errors.As(err)
		}
		cacheFromPath := filepath.Join(mountDir, "cache", sid)
		cacheToPath := workerSB.SectorPath("cache", sid)
		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

func (w *worker) pushCache(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask, unsealedOnly bool) error {
repush:
	select {
	case <-ctx.Done():
		return ffiwrapper.ErrWorkerExit.As(task)
	default:
		// TODO: check cache is support two task
		api, err := GetNodeApi()
		if err != nil {
			log.Warn(errors.As(err))
			goto repush
		}

		w.diskPool.UpdateState(task.SectorName(), database.SECTOR_STATE_PUSH)

		// release the worker when pushing happened
		if err := api.RetryWorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "pushing commit", database.SECTOR_STATE_PUSH); err != nil {
			log.Warn(errors.As(err))

			if errors.ErrNoData.Equal(err) {
				// drop data
				return nil
			}

			goto repush
		}

		if !unsealedOnly {
			if err := w.pushSealed(ctx, workerSB, task); err != nil {
				log.Error(errors.As(err, task))
				time.Sleep(60e9)
				goto repush
			}
		}
		//if task.StoreUnseal {
		//	if err := w.pushUnsealed(ctx, workerSB, task); err != nil {
		//		log.Error(errors.As(err, task))
		//		time.Sleep(60e9)
		//		goto repush
		//	}
		//}
	}
	return nil
}
func (w *worker) removeDataLayer(ctx context.Context, cacheDir string, removeC1cache bool) {
	files := make([]string, 0)
	file, err := os.Stat(cacheDir)
	if err != nil {
		log.Error(err)
		return
	}
	if !file.IsDir() {
		return
	}
	fileInfo, err := ioutil.ReadDir(cacheDir)
	if err != nil {
		log.Error(err)
		return
	}
	if len(fileInfo) == 0 {
		return
	}
	for _, v := range fileInfo {
		if v.IsDir() {
			log.Warn("dir ::  ==> ", cacheDir+v.Name())
		} else {
			if strings.Contains(v.Name(), "data-layer") {
				filename := strings.TrimRight(cacheDir, "/") + "/" + v.Name()
				files = append(files, filename)
			}
			if removeC1cache {
				if strings.Contains(v.Name(), "c1.out") {
					filename := strings.TrimRight(cacheDir, "/") + "/" + v.Name()
					files = append(files, filename)
				}
				if strings.Contains(v.Name(), "data-layer") {
					filename := strings.TrimRight(cacheDir, "/") + "/" + v.Name()
					files = append(files, filename)
				}
				if strings.Contains(v.Name(), "tree-d") {
					filename := strings.TrimRight(cacheDir, "/") + "/" + v.Name()
					files = append(files, filename)
				}
				if strings.Contains(v.Name(), "tree-c") {
					filename := strings.TrimRight(cacheDir, "/") + "/" + v.Name()
					files = append(files, filename)
				}
			}
		}
	}
	for _, filename := range files {
		err := os.Remove(filename)
		if err != nil {
			log.Error(err)
			return
		}
	}
	log.Infof("remove files: %v ", files)
}
func (w *worker) pushUpdateCache(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask, unsealedOnly bool) error {
repush:
	select {
	case <-ctx.Done():
		return ffiwrapper.ErrWorkerExit.As(task)
	default:
		// TODO: check cache is support two task
		api, err := GetNodeApi()
		if err != nil {
			log.Warn(errors.As(err))
			goto repush
		}

		w.diskPool.UpdateState(task.SectorName(), database.SECTOR_STATE_PUSH)

		// release the worker when pushing happened
		if err := api.RetryWorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "pushing commit", database.SECTOR_STATE_PUSH); err != nil {
			log.Warn(errors.As(err))

			if errors.ErrNoData.Equal(err) {
				// drop data
				return nil
			}

			goto repush
		}

		if !unsealedOnly {
			if err := w.pushUpdate(ctx, workerSB, task); err != nil {
				log.Error(errors.As(err, task))
				time.Sleep(60e9)
				goto repush
			}
		}
		if task.StoreUnseal {
			if err := w.pushUnsealed(ctx, workerSB, task); err != nil {
				log.Error(errors.As(err, task))
				time.Sleep(60e9)
				goto repush
			}
		}
		if err := w.RemoveRepoSector(ctx, workerSB.RepoPath(), task.SectorName()); err != nil {
			log.Warn(errors.As(err))
		}
	}
	return nil
}
func (w *worker) pushUpdate(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()
	log.Infof("pushUpdate:%+v", sid)
	defer log.Infof("pushUpdate exit:%+v", sid)

	api, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	ss, err := api.RetryPreStorageNode(ctx, sid, w.workerCfg.IP, database.STORAGE_KIND_SEALED)
	if err != nil {
		return errors.As(err)
	}
	switch ss.MountType {
	case database.MOUNT_TYPE_HLM:
		tmpAuth, err := api.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		// send the sealed
		sealedFromPath := workerSB.SectorPath("update", sid)
		if err := fc.Upload(ctx, sealedFromPath, filepath.Join("update", sid)); err != nil {
			return errors.As(err)
		}
		// send the cache
		cacheFromPath := workerSB.SectorPath("update-cache", sid)
		if err := fc.Upload(ctx, cacheFromPath, filepath.Join("update-cache", sid)); err != nil {
			return errors.As(err)
		}
		if err := api.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	case database.MOUNT_TYPE_OSS:
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		// send the sealed
		sealedFromPath := workerSB.SectorPath("update", sid)
		sealedToPath := filepath.Join(mountDir, "update")
		if err := w.upload(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("update-cache", sid)
		cacheToPath := filepath.Join(mountDir, "update-cache", sid)
		if err := w.upload(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
	case database.MOUNT_TYPE_CUSTOM:
		mountDir := filepath.Join(PushRepoPath, sid)
		//if err := database.MountPostWorker(
		//	ctx,
		//	ss.MountType,
		//	ss.MountSignalUri,
		//	mountDir,
		//	ss.MountOpt,
		//); err != nil {
		//	return errors.As(err, "======MountPostWorker fault", mountDir)
		//}
		// send the sealed
		sealedFromPath := workerSB.SectorPath("update", sid)
		sealedToPath := filepath.Join(mountDir, "update")
		if err := os.MkdirAll(sealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		//// push do a deep checksum
		//hashKind := _HASH_KIND_NONE
		//if w.needMd5Sum {
		//	hashKind = _HASH_KIND_DEEP
		//}
		if err := w.rsync(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("update-cache", sid)
		cacheToPath := filepath.Join(mountDir, "update-cache", sid)
		if err := os.MkdirAll(cacheToPath, 0755); err != nil {
			return errors.As(err)
		}
		// push do a deep checksum
		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}
		//成功之后删除软连接
		//log.Info("pushUpdate remove Link ====", mountDir)
		//os.RemoveAll(mountDir)
	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return errors.As(err)
		}

		// send the sealed
		sealedFromPath := workerSB.SectorPath("update", sid)
		sealedToPath := filepath.Join(mountDir, "update")
		if err := os.MkdirAll(sealedToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			return errors.As(err)
		}

		// send the cache
		cacheFromPath := workerSB.SectorPath("update-cache", sid)
		cacheToPath := filepath.Join(mountDir, "update-cache", sid)
		if err := os.MkdirAll(cacheToPath, 0755); err != nil {
			return errors.As(err)
		}
		if err := w.rsync(ctx, cacheFromPath, cacheToPath); err != nil {
			return errors.As(err)
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return errors.As(err)
		}
	}

	if err := api.RetryCommitStorageNode(ctx, sid, database.STORAGE_KIND_SEALED); err != nil {
		return errors.As(err)
	}
	return nil
}
func (w *worker) FetchStaging(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) (string, error) {
	log.Infof("FetchStaging: %+v", task)
	var tmpFile string
	var err error
refetch:
	select {
	case <-ctx.Done():
		return tmpFile, ffiwrapper.ErrWorkerExit.As(task)
	default:
		tmpFile, err = w.fetchStaging(ctx, workerSB, task)
		if err != nil {
			log.Warn(errors.As(err))
			time.Sleep(10e9)
			goto refetch
		}
	}
	return tmpFile, errors.As(err)
}
func (w *worker) fetchStaging(ctx context.Context, workerSB *ffiwrapper.Sealer, task ffiwrapper.WorkerTask) (string, error) {
	sid := task.SectorName()
	log.Infof("fetchStaging:%+v,%+v", sid, task.PieceData)
	defer log.Infof("fetchStaging exit:%+v", sid)

	oriReaderKind := task.PieceData.ReaderKind
	if oriReaderKind != shared.PIECE_DATA_KIND_SERVER {
		// not a server kind
		return "", nil
	}
	tmpFile := filepath.Join(workerSB.SectorPath("fstmp", sid), task.PieceData.ServerFileName)
	if task.PieceData.ServerStorage == 0 {
		// download to local
		uri := fmt.Sprintf("http://"+w.minerEndpoint+"/file/deal-staging/%s", task.PieceData.ServerFileName)
		if err := w.fetchRemoteFile(uri, tmpFile); err != nil {
			return tmpFile, errors.As(err, uri, tmpFile)
		}
		return tmpFile, nil
	}

	nodeApi, err := GetNodeApi()
	if err != nil {
		return tmpFile, errors.As(err)
	}
	ss, err := nodeApi.RetryGetStorage(ctx, task.PieceData.ServerStorage)
	if err != nil {
		return tmpFile, errors.As(err)
	}
	switch ss.MountType {
	case database.MOUNT_TYPE_CUSTOM:
		// fetch the unsealed file
		mountDir := filepath.Join(w.sealedRepo, fmt.Sprintf("%d", ss.ID))
		fromPath := filepath.Join(mountDir, "deal-staging", task.PieceData.ServerFileName)
		// fetch do a quick checksum
		if err := w.rsync(ctx, fromPath, tmpFile); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return tmpFile, errors.As(err)
			}
			// no data to fetch.
		}
	case database.MOUNT_TYPE_PB:
		server, err := ParseFileOpt(task.PieceData.ServerFullUri)
		if err != nil {
			return tmpFile, errors.As(err)
		}
		type File struct {
			FileId string `json:"file_id"`
		}
		fileId, err := json.Marshal(&File{FileId: server.FileId})
		if err != nil {
			return tmpFile, errors.As(err)
		}
		if err := os.MkdirAll(filepath.Dir(tmpFile), 0755); err != nil {
			return tmpFile, errors.As(err)
		}
		if err := BHFetch(server.FileRemote, string(fileId), tmpFile, true); err != nil {
			return tmpFile, errors.As(err)
		}
	case database.MOUNT_TYPE_HLM:
		tmpAuth, err := nodeApi.RetryNewHLMStorageTmpAuth(ctx, ss.ID, sid)
		if err != nil {
			return tmpFile, errors.As(err)
		}
		fc := hlmclient.NewHttpClient(ss.MountTransfUri, sid, tmpAuth)

		if err := fc.Download(ctx, tmpFile, filepath.Join("deal-staging", task.PieceData.ServerFileName)); err != nil {
			if !os.IsNotExist(err) {
				return tmpFile, errors.As(err)
			}
			// it's ok if the unsealed not exist.
		}
		if err := nodeApi.RetryDelHLMStorageTmpAuth(ctx, ss.ID, sid); err != nil {
			log.Warn(errors.As(err))
		}
	default:
		mountUri := ss.MountTransfUri
		mountDir := filepath.Join(w.sealedRepo, sid)
		if err := w.mountRemote(
			ctx,
			sid,
			ss.MountType,
			mountUri,
			mountDir,
			ss.MountOpt,
		); err != nil {
			return tmpFile, errors.As(err)
		}

		// fetch the staging file
		//fromPath := filepath.Join(mountDir, "deal-staging", task.PieceData.ServerFileName)
		// fetch do a quick checksum
		fromPath := filepath.Join(mountDir, task.PieceData.ServerFileName)
		log.Infof("from : %+v => to : %+v", fromPath, tmpFile)
		if err := w.rsync(ctx, fromPath, tmpFile); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return tmpFile, errors.As(err)
			}
			// no data to fetch.
		}

		if err := w.umountRemote(sid, mountDir); err != nil {
			return tmpFile, errors.As(err)
		}
	}
	return tmpFile, nil
}
func (w *worker) ConfirmStaging(ctx context.Context, workerSB *ffiwrapper.Sealer, sid string) error {
	newCtx, _ := context.WithCancel(ctx)
	nodeApi, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	deals, err := nodeApi.RetryGetMarketDealInfoBySid(newCtx, sid)
	if err != nil {
		return errors.As(err)
	}
	for _, deal := range deals {
		if err := w.confirmStaging(newCtx, workerSB, sid, storiface.PieceData{
			ReaderKind:    shared.PIECE_DATA_KIND_SERVER,
			LocalPath:     deal.FileLocal,
			UnpaddedSize:  abi.UnpaddedPieceSize(deal.PieceSize),
			ServerStorage: deal.FileStorage,
			ServerFullUri: deal.FileRemote,
			PropCid:       deal.ID,
		}); err != nil {
			return errors.As(err)
		}
	}
	return nil
}
func (w *worker) confirmStaging(ctx context.Context, workerSB *ffiwrapper.Sealer, sid string, pieceData storiface.PieceData) error {
	log.Infof("confirmStaging:%+v", sid, pieceData)
	defer log.Infof("confirmStaging exit:%+v", sid)
	if pieceData.ServerStorage == 0 {
		return nil
	}
	nodeApi, err := GetNodeApi()
	if err != nil {
		return errors.As(err)
	}
	ss, err := nodeApi.GetStorage(ctx, pieceData.ServerStorage)
	if err != nil {
		return errors.As(err)
	}
	switch ss.MountType {
	case database.MOUNT_TYPE_PB:
		server, err := ParseFileOpt(pieceData.ServerFullUri)
		if err != nil {
			return errors.As(err)
		}
		type File struct {
			FileId string `json:"file_id"`
		}
		fileId, err := json.Marshal(&File{FileId: server.FileId})
		if err != nil {
			return errors.As(err)
		}
		if err := BHConfirm(server.FileRemote, string(fileId), sid); err != nil {
			return errors.As(err)
		}
	default:
		return nil
	}
	return nil
}
