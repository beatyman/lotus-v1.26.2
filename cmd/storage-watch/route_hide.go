package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/hide", hideHandler)
	RegisterHandle("/show", showHandler)
}

func hideHandler(w http.ResponseWriter, r *http.Request) error {
	sid := r.FormValue("sid")
	if len(sid) == 0 {
		return writeMsg(w, 404, "sector not found")
	}

	for _, repo := range repos {
		file := filepath.Join(repo, "cache", sid, "p_aux")
		_, err := os.Stat(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return errors.As(err)
		}

		// exist
		// unlock
		if err := chattrUnlock(filepath.Join(repo, "cache", sid)); err != nil {
			return errors.As(err)
		}

		bakFile := filepath.Join(repo, "cache", sid, "p_aux.bak")
		log.Infof("hide file:%s,%s", file, bakFile)
		if err := os.Rename(file, bakFile); err != nil {
			return errors.As(err)
		}

		// relock
		if err := chattrLock(filepath.Join(repo, "cache", sid)); err != nil {
			return errors.As(err)
		}
		return writeMsg(w, 200, "OK")
	}
	return writeMsg(w, 404, "file not found")
}

func showHandler(w http.ResponseWriter, r *http.Request) error {
	sid := r.FormValue("sid")
	if len(sid) == 0 {
		return writeMsg(w, 404, "sector not found")
	}

	for _, repo := range repos {
		bakFile := filepath.Join(repo, "cache", sid, "p_aux.bak")
		_, err := os.Stat(bakFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return errors.As(err)
		}

		// exist

		// unlock
		if err := chattrUnlock(filepath.Join(repo, "cache", sid)); err != nil {
			return errors.As(err)
		}
		file := filepath.Join(repo, "cache", sid, "p_aux")
		log.Infof("show file:%s,%s", bakFile, file)
		if err := os.Rename(bakFile, file); err != nil {
			return errors.As(err)
		}

		// relock
		if err := chattrLock(filepath.Join(repo, "cache", sid)); err != nil {
			return errors.As(err)
		}
		return writeMsg(w, 200, "OK")
	}
	return writeMsg(w, 404, "file not found")
}
