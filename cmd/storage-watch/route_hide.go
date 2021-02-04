package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/hide", hideHandler)
	RegisterHandle("/show", showHandler)
}

func hideHandler(w http.ResponseWriter, r *http.Request) error {
	sectors := r.FormValue("sectors")
	if len(sectors) == 0 {
		return writeMsg(w, 404, "sector not found")
	}
	sids := strings.Split(sectors, ",")

	for _, repo := range repos {
		for _, sid := range sids {
			// unlock
			if err := chattrUnlock(filepath.Join(repo, "cache", sid)); err != nil {
				return errors.As(err)
			}
			defer func() {
				// relock
				if err := chattrLock(filepath.Join(repo, "cache", sid)); err != nil {
					log.Error(errors.As(err))
				}
			}()
			file := filepath.Join(repo, "cache", sid, "p_aux")
			_, err := os.Stat(file)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return errors.As(err)
			}

			// exist
			bakFile := filepath.Join(repo, "cache", sid, "p_aux.bak")
			log.Infof("hide file:%s,%s", file, bakFile)
			if err := os.Rename(file, bakFile); err != nil {
				return errors.As(err)
			}
		}
	}
	return writeMsg(w, 200, "OK")
}

func showHandler(w http.ResponseWriter, r *http.Request) error {
	sectors := r.FormValue("sectors")
	if len(sectors) == 0 {
		return writeMsg(w, 404, "sector not found")
	}
	sids := strings.Split(sectors, ",")

	for _, repo := range repos {
		for _, sid := range sids {
			// unlock
			if err := chattrUnlock(filepath.Join(repo, "cache", sid)); err != nil {
				return errors.As(err)
			}
			defer func() {
				// relock
				if err := chattrLock(filepath.Join(repo, "cache", sid)); err != nil {
					log.Error(errors.As(err))
				}
			}()

			bakFile := filepath.Join(repo, "cache", sid, "p_aux.bak")
			_, err := os.Stat(bakFile)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return errors.As(err)
			}

			// exist
			file := filepath.Join(repo, "cache", sid, "p_aux")
			log.Infof("show file:%s,%s", bakFile, file)
			if err := os.Rename(bakFile, file); err != nil {
				return errors.As(err)
			}
		}
	}
	return writeMsg(w, 200, "OK")
}
