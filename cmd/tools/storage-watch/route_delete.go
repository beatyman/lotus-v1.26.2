package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/delete", deleteHandler)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) error {
	sid := r.FormValue("sid")
	if len(sid) == 0 {
		return writeMsg(w, 404, "sector not found")
	}

	for _, repo := range repos {
		cacheFile := filepath.Join(repo, "cache", sid)
		sealedFile := filepath.Join(repo, "sealed", sid)
		unsealedFile := filepath.Join(repo, "unsealed", sid)

		// TODO:stop protect.
		if err := os.RemoveAll(cacheFile); err != nil {
			if !os.IsNotExist(err) {
				return errors.As(err)
			}
		} else {
			log.Warnf("delete file:%s", cacheFile)
		}

		if err := os.Remove(sealedFile); err != nil {
			if !os.IsNotExist(err) {
				return errors.As(err)
			}
		} else {
			log.Warnf("delete file:%s", sealedFile)
		}
		if err := os.Remove(unsealedFile); err != nil {
			if !os.IsNotExist(err) {
				return errors.As(err)
			}
		} else {
			log.Warnf("delete file:%s", unsealedFile)
		}

	}
	return writeMsg(w, 200, "OK")
}
