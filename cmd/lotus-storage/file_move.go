package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/file/move", moveHandler)
}

func moveHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, true)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}

	newPath := r.FormValue("new")
	if !validHttpFilePath(newPath) {
		return writeMsg(w, 403, "error filepath")
	}
	repo := _repoFlag
	oldName := filepath.Join(repo, file)
	newName := filepath.Join(repo, newPath)
	if err := os.Rename(oldName, newName); err != nil {
		return errors.As(err)
	}
	log.Infof("Rename file %s to %s, from %s", file, newPath, r.RemoteAddr)
	return nil
}
