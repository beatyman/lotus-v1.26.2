package main

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/gwaylib/errors"
)

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
	if err := w.diskPool.Delete(sid); err != nil {
		log.Error(errors.As(err))
	}
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
		if err := w.pushUnsealed(ctx, workerSB, task); err != nil {
			log.Error(errors.As(err, task))
			time.Sleep(60e9)
			goto repush
		}
		if err := w.RemoveRepoSector(ctx, workerSB.RepoPath(), task.SectorName()); err != nil {
			log.Warn(errors.As(err))
		}
	}
	return nil
}
