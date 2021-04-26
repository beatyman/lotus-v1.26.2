package main

import (
	"net/http"
	"syscall"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/file/capacity", capacityHandler)
}

func capacityHandler(w http.ResponseWriter, r *http.Request) error {
	if _, ok := authFile(r, false); !ok {
		return writeMsg(w, 401, "auth failed")
	}

	// implement the df -h
	root := _repoFlag
	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(root, &fs); err != nil {
		return errors.As(err, root)
	}

	return writeJson(w, 200, fs)
}
