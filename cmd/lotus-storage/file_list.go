package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/file/stat", statHandler)
	RegisterHandle("/file/list", listHandler)
}

func statHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, false)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}

	repo := _repoFlag
	path := filepath.Join(repo, file)

	fStat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return writeMsg(w, 404, "filepath not exist")
		}
		return errors.As(err, path)
	}
	return writeJson(w, 200, &utils.ServerFileStat{
		FileName:    ".",
		IsDirFile:   fStat.IsDir(),
		FileSize:    fStat.Size(),
		FileModTime: fStat.ModTime(),
	})
}

func listHandler(w http.ResponseWriter, r *http.Request) error {
	file, ok := authFile(r, false)
	if !ok {
		return writeMsg(w, 401, "auth failed")
	}

	repo := _repoFlag
	path := filepath.Join(repo, file)

	fStat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return writeMsg(w, 404, "filepath not exist")
		}
		return errors.As(err, path)
	}
	if !fStat.IsDir() {
		return writeJson(w, 200, []utils.ServerFileStat{
			utils.ServerFileStat{
				FileName:    ".",
				IsDirFile:   false,
				FileSize:    fStat.Size(),
				FileModTime: fStat.ModTime(),
			},
		})
	}
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return errors.As(err)
	}
	result := []utils.ServerFileStat{}
	for _, fs := range dirs {
		size := int64(0)
		if !fs.IsDir() {
			size = fs.Size()
		}
		result = append(result, utils.ServerFileStat{
			FileName:    fs.Name(),
			IsDirFile:   fs.IsDir(),
			FileSize:    size,
			FileModTime: fs.ModTime(),
		})
	}
	return writeJson(w, 200, result)
}
