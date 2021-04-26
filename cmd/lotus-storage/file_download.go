package main

import (
	"net/http"
	"path/filepath"
)

func init() {
	RegisterHandle("/file/download", downloadHandler)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, false)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}
	repo := _repoFlag
	to := filepath.Join(repo, file)
	http.ServeFile(w, r, to)
	return nil
}
