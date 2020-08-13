package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	//"path/filepath"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/gwaylib/errors"
)

const (
	PARAMS_PATH = "/file/filecoin-proof-parameters"
)

func (w *worker) FetchHlmParams(ctx context.Context, napi api.StorageMiner, endpoint string) error {
	paramUri := ""
	// try download from worker
	dlWorker, err := napi.WorkerPreConn(ctx)
	if err != nil {
		if !errors.ErrNoData.Equal(err) {
			return errors.As(err)
		}
		// pass, using miner's
	} else {
		if dlWorker.SvcConn < 2 {
			paramUri = "http://" + dlWorker.SvcUri + PARAMS_PATH
		}
		// else using miner's
	}
	// try download from miner
	if len(paramUri) == 0 {
		minerConns, err := napi.WorkerMinerConn(ctx)
		if err != nil {
			return errors.As(err)
		}
		// no worker online, get from miner
		if minerConns > 10 {
			return errors.New("miner download connections full")
		}
		paramUri = "http://" + endpoint + PARAMS_PATH
	}

	for {
		log.Info("try fetch hlm params")
		if err := w.tryFetchParams(paramUri, "/var/tmp/filecoin-proof-parameters"); err != nil {
			log.Warn(errors.As(err))
			time.Sleep(10e9)
			continue
		}
		return nil
	}
}

func (w *worker) tryFetchParams(serverUri, to string) error {
	var err error
	for i := 0; i < 3; i++ {
		err = w.fetchParams(serverUri, to)
		if err != nil {
			log.Warn(errors.As(err, serverUri, to))
			continue
		}
		return nil
	}
	return err
}

func (w *worker) fetchParams(serverUri, to string) error {
	// fetch cache
	cacheResp, err := http.Get(serverUri)
	if err != nil {
		return errors.As(err)
	}
	defer cacheResp.Body.Close()
	if cacheResp.StatusCode != 200 {
		return errors.New(cacheResp.Status).As(serverUri)
	}
	cacheRespData, err := ioutil.ReadAll(cacheResp.Body)
	if err != nil {
		return errors.As(err)
	}
	cacheDir := &fileserver.StorageDirectoryResp{}
	if err := xml.Unmarshal(cacheRespData, cacheDir); err != nil {
		return errors.As(err)
	}
	if err := os.MkdirAll(to, 0755); err != nil {
		return errors.As(err)
	}
	for _, file := range cacheDir.Files {
		from := fmt.Sprintf("%s/%s", serverUri, file.Value)
		to := filepath.Join(to, file.Value)
		if err := w.fetchRemoteFile(
			from,
			to,
		); err != nil {
			return errors.As(err)
		}
	}
	return nil
}
