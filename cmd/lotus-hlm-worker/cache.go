package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/gwaylib/errors"
)

// remove cache of the sector
func (w *worker) RemoveCache(ctx context.Context, repo, sid string) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()
	return w.removeCache(ctx, repo, sid)
}

// clean cache of the unusing sector.
func (w *worker) CleanCache(ctx context.Context) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	repos := w.diskPool.Repos()

	for _, repo := range repos {
		if err := w.cleanCache(ctx, repo, "sealed"); err != nil {
			return errors.As(err)
		}
		if err := w.cleanCache(ctx, repo, "cache"); err != nil {
			return errors.As(err)
		}
		if err := w.cleanCache(ctx, repo, "unsealed"); err != nil {
			return errors.As(err)
		}
		//delete(w.sealers, repo)
	}
	return nil
}

func (w *worker) removeCache(ctx context.Context, repo, sid string) error {
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

func (w *worker) cleanCache(ctx context.Context, repo, typ string) error {
	dir := filepath.Join(repo, typ)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		fileNames := []string{}
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		ws, err := api.WorkerWorkingById(ctx, fileNames)
		if err != nil {
			ReleaseNodeApi(false)
			return errors.As(err, fileNames)
		}
	sealedLoop:
		for _, f := range files {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue sealedLoop
				}
			}
			if err := w.removeCache(ctx, repo, f.Name()); err != nil {
				return errors.As(err, w.workerCfg.IP, repo, typ, f.Name())
			}
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
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
	ss, err := api.PreStorageNode(ctx, sid, w.workerCfg.IP, database.STORAGE_KIND_SEALED)
	if err != nil {
		return errors.As(err)
	}
	mountUri := ss.MountTransfUri
	if strings.Index(mountUri, w.workerCfg.IP) > -1 {
		log.Infof("found local storage, chagne %s to mount local", mountUri)
		// fix to 127.0.0.1 if it has the same ip.
		mountUri = strings.Replace(mountUri, w.workerCfg.IP, "127.0.0.1", -1)
	}
	mountDir := filepath.Join(w.sealedRepo, sid)
	if err := w.mountRemote(
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
	if err := api.CommitStorageNode(ctx, sid, database.STORAGE_KIND_SEALED); err != nil {
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

	mountUri := ss.MountTransfUri

	if strings.Index(mountUri, w.workerCfg.IP) > -1 {
		log.Infof("found local storage, chagne %s to mount local", mountUri)
		// fix to 127.0.0.1 if it has the same ip.
		mountUri = strings.Replace(mountUri, w.workerCfg.IP, "127.0.0.1", -1)
	}
	mountDir := filepath.Join(w.sealedRepo, sid)
	if err := w.mountRemote(
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

	mountUri := ss.MountTransfUri

	if strings.Index(mountUri, w.workerCfg.IP) > -1 {
		log.Infof("found local storage, chagne %s to mount local", mountUri)
		// fix to 127.0.0.1 if it has the same ip.
		mountUri = strings.Replace(mountUri, w.workerCfg.IP, "127.0.0.1", -1)
	}
	mountDir := filepath.Join(w.sealedRepo, sid)
	if err := w.mountRemote(
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

	mountUri := ss.MountTransfUri

	if strings.Index(mountUri, w.workerCfg.IP) > -1 {
		log.Infof("found local storage, chagne %s to mount local", mountUri)
		// fix to 127.0.0.1 if it has the same ip.
		mountUri = strings.Replace(mountUri, w.workerCfg.IP, "127.0.0.1", -1)
	}
	mountDir := filepath.Join(w.sealedRepo, sid)
	if err := w.mountRemote(
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
		// release the worker when pushing happened
		if err := api.WorkerUnlock(ctx, w.workerCfg.ID, task.Key(), "pushing commit", database.SECTOR_STATE_PUSH); err != nil {
			log.Warn(errors.As(err))

			if errors.ErrNoData.Equal(err) {
				// drop data
				return nil
			}

			ReleaseNodeApi(false)
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
		if err := w.RemoveCache(ctx, workerSB.RepoPath(), task.SectorName()); err != nil {
			log.Warn(errors.As(err))
		}
	}
	return nil
}
