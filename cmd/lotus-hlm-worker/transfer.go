package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

func (w *worker) deleteRemoteCache(uri, sid, typ string) error {
	data := url.Values{}
	data.Set("sid", sid)
	data.Set("type", typ)
	url := fmt.Sprintf("%s/file/storage/delete", uri)
	//log.Info("url:", url)
	req, err := http.NewRequest("POST", url, strings.NewReader(data.Encode()))
	if err != nil {
		return errors.As(err, ":request")
	}
	req.Header = w.auth
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.As(err, url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New(resp.Status).As(resp.StatusCode, uri, sid)
	}
	return nil
}

func (w *worker) fetchRemoteFile(uri, to string) error {
	log.Infof("fetch file from %s to %s", uri, to)
	file, err := os.OpenFile(to, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.As(err, uri, to)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return errors.As(err, uri, to)
	}
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return errors.As(err, uri, to)
	}
	req.Header = w.auth
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", stat.Size()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.As(err, uri, to)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 206:
		if _, err := io.Copy(file, resp.Body); err != nil {
			return errors.As(err, uri, to)
		}
	case 416:
		return nil
	case 404:
		return errors.ErrNoData.As(uri, to)
	default:
		return errors.New(resp.Status).As(resp.StatusCode, uri, to)
	}
	return nil
}

func (w *worker) fetchRemote(serverUri string, sectorID string, typ ffiwrapper.WorkerTaskType) error {
	var err error
	for i := 0; i < 3; i++ {
		err = w.tryFetchRemote(serverUri, sectorID, typ)
		if err != nil {
			log.Warn(errors.As(err, i, serverUri, sectorID, typ))
			continue
		}
		return nil
	}
	return err
}
func (w *worker) tryFetchRemote(serverUri string, sectorID string, typ ffiwrapper.WorkerTaskType) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}
	switch typ {
	case ffiwrapper.WorkerPreCommit1:
		// fetch unsealed
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/unsealed/%s", serverUri, sectorID),
			filepath.Join(w.workerRepo, "unsealed", sectorID),
		); err != nil {
			return errors.As(err, typ)
		}

	default:
		// fetch unsealed, prepare for p2 or c2 failed to p1, p1 need the unsealed.
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/unsealed/%s", serverUri, sectorID),
			filepath.Join(w.workerRepo, "unsealed", sectorID),
		); err != nil {
			return errors.As(err, typ)
		}

		// fetch cache
		url := fmt.Sprintf("%s/file/storage/cache/%s/", serverUri, sectorID)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return errors.As(err, url)
		}
		req.Header = w.auth
		cacheResp, err := http.DefaultClient.Do(req)
		if err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
		defer cacheResp.Body.Close()
		if cacheResp.StatusCode != 200 {
			return errors.New(cacheResp.Status).As(serverUri, sectorID, typ)
		}
		cacheRespData, err := ioutil.ReadAll(cacheResp.Body)
		if err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
		cacheDir := &fileserver.StorageDirectoryResp{}
		if err := xml.Unmarshal(cacheRespData, cacheDir); err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
		if err := os.MkdirAll(filepath.Join(w.workerRepo, "cache", sectorID), 0755); err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
		for _, file := range cacheDir.Files {
			if err := w.fetchRemoteFile(
				fmt.Sprintf("%s/file/storage/cache/%s/%s", serverUri, sectorID, file.Value),
				filepath.Join(w.workerRepo, "cache", sectorID, file.Value),
			); err != nil {
				return errors.As(err, serverUri, sectorID, typ)
			}
		}

		// fetch sealed
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/sealed/%s", serverUri, sectorID),
			filepath.Join(w.workerRepo, "sealed", sectorID),
		); err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
	}

	return nil
}

func (w *worker) rsync(ctx context.Context, fromPath, toPath string) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	stat, err := os.Stat(fromPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(fromPath)
		}
		return err
	}

	log.Infof("rsync, from: %s, to: %s", fromPath, toPath)
	if stat.IsDir() {
		if err := CopyFile(ctx, fromPath+"/", toPath+"/"); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, fromPath, toPath); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) remove(typ string, sectorID abi.SectorID) error {
	filename := filepath.Join(w.workerRepo, typ, storage.SectorName(sectorID))
	log.Infof("Remove file: %s", filename)
	return os.RemoveAll(filename)
}
