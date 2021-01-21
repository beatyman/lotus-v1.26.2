package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

func (w *worker) mountRemote(sid, mountType, mountUri, mountDir, mountOpt string) error {
	// mount
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return errors.As(err, mountDir)
	}
	w.pushMu.Lock()
	w.sealedMounted[sid] = mountDir
	mountedData, err := json.Marshal(w.sealedMounted)
	if err != nil {
		w.pushMu.Unlock()
		return errors.As(err, w.sealedMountedFile)
	}
	if err := ioutil.WriteFile(w.sealedMountedFile, mountedData, 0666); err != nil {
		w.pushMu.Unlock()
		return errors.As(err, w.sealedMountedFile)
	}
	w.pushMu.Unlock()

	// a fix point, link or mount to the targe file.
	if err := database.Mount(
		mountType,
		mountUri,
		mountDir,
		mountOpt,
	); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) umountRemote(sid, mountDir string) error {
	// umount and client the tmp file
	if _, err := database.Umount(mountDir); err != nil {
		return errors.As(err)
	}
	log.Infof("Remove mount point:%s", mountDir)
	if err := os.Remove(mountDir); err != nil {
		return errors.As(err)
	}

	w.pushMu.Lock()
	delete(w.sealedMounted, sid)
	mountedData, err := json.Marshal(w.sealedMounted)
	if err != nil {
		w.pushMu.Unlock()
		return errors.As(err)
	}
	if err := ioutil.WriteFile(w.sealedMountedFile, mountedData, 0666); err != nil {
		w.pushMu.Unlock()
		return errors.As(err)
	}
	w.pushMu.Unlock()
	return nil
}

func umountAllRemote(sealedMountedFile string) error {
	sealedMounted := map[string]string{}
	if mountedData, err := ioutil.ReadFile(sealedMountedFile); err == nil {
		if err := json.Unmarshal(mountedData, &sealedMounted); err != nil {
			return errors.As(err, sealedMountedFile)
		}
		for _, p := range sealedMounted {
			if _, err := database.Umount(p); err != nil {
				log.Info(err)
			} else {
				if err := os.Remove(p); err != nil {
					log.Error(err)
				}
			}
		}
		return nil
	} else {
		// drop the file error
		log.Info(errors.As(err))
	}
	return nil
}
