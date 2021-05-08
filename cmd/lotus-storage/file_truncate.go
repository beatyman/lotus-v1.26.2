package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/file/truncate", truncateHandler)
}

func truncateHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, true)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}

	size, err := strconv.ParseInt(r.FormValue("size"), 10, 64)
	if err != nil {
		return writeMsg(w, 403, "file size failed")
	}

	repo := _repoFlag
	path := filepath.Join(repo, file)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return errors.As(err, path)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644) // nolint
	if err != nil {
		return errors.As(err, path)
	}
	defer f.Close()

	log.Infof("Trucate %s, size:%d, from:%s", file, size, r.RemoteAddr)
	if err := f.Truncate(size); err != nil {
		return errors.As(err, path)
	}
	return nil
}
