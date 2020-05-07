package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/gwaylib/errors"
)

func fetchFile(uri, to string) error {
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
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", stat.Size()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.As(err, uri, to)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("server error").As(resp.StatusCode, uri, to)
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		return errors.As(err, uri, to)
	}
	return nil
}

func (w *worker) fetch(serverUri string, sectorID string) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

	// fetch cache
	cacheResp, err := http.Get(fmt.Sprintf("%s/cache/%s/", serverUri, sectorID))
	if err != nil {
		return errors.As(err)
	}
	defer cacheResp.Body.Close()
	cacheRespData, err := ioutil.ReadAll(cacheResp.Body)
	if err != nil {
		return errors.As(err)
	}
	cacheDir := &StorageDirectoryResp{}
	if err := xml.Unmarshal(cacheRespData, cacheDir); err != nil {
		return errors.As(err)
	}
	if err := os.MkdirAll(filepath.Join(w.repo, "cache", sectorID), 0755); err != nil {
		return errors.As(err)
	}
	for _, file := range cacheDir.Files {
		if err := fetchFile(
			fmt.Sprintf("%s/cache/%s/%s", serverUri, sectorID, file.Value),
			filepath.Join(w.repo, "cache", sectorID, file.Value),
		); err != nil {
			return errors.As(err)
		}
	}

	// fetch sealed
	if err := fetchFile(
		fmt.Sprintf("%s/sealed/%s", serverUri, sectorID),
		filepath.Join(w.repo, "sealed", sectorID),
	); err != nil {
		return errors.As(err)
	}

	// fetch unsealed
	if err := fetchFile(
		fmt.Sprintf("%s/unsealed/%s", serverUri, sectorID),
		filepath.Join(w.repo, "unsealed", sectorID),
	); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) push(ctx context.Context, typ string, sectorID string) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

	fromPath := w.sb.SectorPath(typ, sectorID)
	stat, err := os.Stat(string(fromPath))
	if err != nil {
		return err
	}

	// save to target storage
	toPath := w.sealedSB.SectorPath(typ, sectorID)

	if stat.IsDir() {
		if err := CopyFile(ctx, string(fromPath)+"/", string(toPath)+"/"); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, string(fromPath), string(toPath)); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) remove(typ string, sectorID abi.SectorID) error {
	filename := filepath.Join(w.repo, typ, w.sb.SectorName(sectorID))
	log.Infof("Remove file: %s", filename)
	return os.RemoveAll(filename)
}
