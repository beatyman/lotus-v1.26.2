package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/file/delete", deleteHandler)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, true)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}

	repo := _repoFlag
	path := filepath.Join(repo, file)
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err)
		}
	}
	log.Warnf("Remove file:%s, from:%s", path, r.RemoteAddr)
	return nil
}
