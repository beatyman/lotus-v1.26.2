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
func (w *worker) RemoveCache(ctx context.Context, sid string) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	log.Infof("Remove cache:%s,%s", w.workerRepo, sid)
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "sealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "cache", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	if err := os.RemoveAll(filepath.Join(w.workerRepo, "unsealed", sid)); err != nil {
		log.Error(errors.As(err, sid))
	}
	return nil
}

// clean cache of the unusing sector.
func (w *worker) CleanCache(ctx context.Context) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	// not do this on miner repo
	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	sealed := filepath.Join(w.workerRepo, "sealed")
	cache := filepath.Join(w.workerRepo, "cache")
	// staged := filepath.Join(w.workerRepo, "staging")
	unsealed := filepath.Join(w.workerRepo, "unsealed")
	if err := w.cleanCache(ctx, sealed); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, cache); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, unsealed); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) cleanCache(ctx context.Context, path string) error {
	files, err := ioutil.ReadDir(path)
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
			log.Infof("Remove %s", filepath.Join(path, f.Name()))
			if err := os.RemoveAll(filepath.Join(path, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(path, f.Name()))
			}
		}
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	return nil
}

func (w *worker) pushSealed(ctx context.Context, task ffiwrapper.WorkerTask) error {
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
	sealedFromPath := w.workerSB.SectorPath("sealed", sid)
	sealedToPath := filepath.Join(mountDir, "sealed")
	if err := os.MkdirAll(sealedToPath, 0755); err != nil {
		return errors.As(err)
	}
	if err := w.rsync(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
		return errors.As(err)
	}

	// send the cache
	cacheFromPath := w.workerSB.SectorPath("cache", sid)
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
func (w *worker) pushUnsealed(ctx context.Context, task ffiwrapper.WorkerTask) error {
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

	unsealedFromPath := w.workerSB.SectorPath("unsealed", sid)
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

func (w *worker) fetchPledge(ctx context.Context, task ffiwrapper.WorkerTask) error {
	sid := task.SectorName()
	log.Infof("fetchPledge:%+v", sid)
	defer log.Infof("fetchPledge exit:%+v", sid)

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

	// fetch the unsealed file
	unsealedFromPath := filepath.Join(mountDir, "unsealed")
	unsealedToPath := w.workerSB.SectorPath("unsealed", sid)
	if err := w.rsync(ctx, filepath.Join(unsealedFromPath, sid), unsealedToPath); err != nil {
		return errors.As(err)
	}

	if err := w.umountRemote(sid, mountDir); err != nil {
		return errors.As(err)
	}
	return nil

}

func (w *worker) pushCache(ctx context.Context, task ffiwrapper.WorkerTask) error {
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

		if err := w.pushSealed(ctx, task); err != nil {
			log.Error(errors.As(err, task))
			time.Sleep(60e9)
			goto repush
		}
		if err := w.pushUnsealed(ctx, task); err != nil {
			log.Error(errors.As(err, task))
			time.Sleep(60e9)
			goto repush
		}
		if err := w.RemoveCache(ctx, task.SectorName()); err != nil {
			log.Warn(errors.As(err))
		}
	}
	return nil
}
