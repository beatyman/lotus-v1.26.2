package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	//"path/filepath"

	"github.com/dchest/blake2b"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/gwaylib/errors"
	"golang.org/x/xerrors"
)

const (
	PARAMS_PATH = "/file/filecoin-proof-parameters"
)

var checked = map[string]struct{}{}
var checkedLk sync.Mutex

type paramFile struct {
	Cid        string `json:"cid"`
	Digest     string `json:"digest"`
	SectorSize uint64 `json:"sector_size"`
}

func checkParams(ctx context.Context, paramBytes []byte, storageSize uint64, paramDir string) error {
	var params map[string]paramFile
	if err := json.Unmarshal(paramBytes, &params); err != nil {
		return err
	}
	for name, info := range params {
		if storageSize != info.SectorSize && strings.HasSuffix(name, ".params") {
			continue
		}
		path := filepath.Join(paramDir, name)
		if err := checkFile(path, info); err != nil {
			return err
		}
	}
	return nil
}
func checkFile(path string, info paramFile) error {
	if os.Getenv("TRUST_PARAMS") == "1" {
		log.Warn("Assuming parameter files are ok. DO NOT USE IN PRODUCTION")
		return nil
	}

	checkedLk.Lock()
	_, ok := checked[path]
	checkedLk.Unlock()
	if ok {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := blake2b.New512()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	sum := h.Sum(nil)
	strSum := hex.EncodeToString(sum[:16])
	if strSum == info.Digest {
		log.Infof("Parameter file %s is ok", path)

		checkedLk.Lock()
		checked[path] = struct{}{}
		checkedLk.Unlock()

		return nil
	}

	return xerrors.Errorf("checksum mismatch in param file %s, %s != %s", path, strSum, info.Digest)
}

func (w *worker) FetchHlmParams(ctx context.Context, napi api.StorageMiner, endpoint, to string, ssize uint64) error {
	//if err := paramfetch.GetParams(ctx, build.ParametersJSON(), ssize); err != nil {
	//	return errors.As(err)
	//}
	if err := checkParams(ctx, build.ParametersJSON(), ssize, to); err == nil {
		return nil
	} else {
		log.Warn(errors.As(err))
	}
	for {
		log.Info("try fetch hlm params")
		if err := w.tryFetchParams(ctx, napi, endpoint, to); err != nil {
			log.Info(errors.As(err))
			time.Sleep(10e9)
			continue
		}
		return nil
	}
}

func (w *worker) tryFetchParams(ctx context.Context, napi api.StorageMiner, endpoint, to string) error {
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
			defer napi.WorkerAddConn(ctx, dlWorker.ID, -1) // return preconn
		} else {
			napi.WorkerAddConn(ctx, dlWorker.ID, -1) // return preconn
			// else using miner's
		}
	}

	// try download from miner
	if len(paramUri) == 0 {
		minerConns, err := napi.WorkerMinerConn(ctx)
		if err != nil {
			return errors.As(err)
		}
		// no worker online, get from miner
		if minerConns > 3 {
			return errors.New("miner download connections full")
		}
		paramUri = "http://" + endpoint + PARAMS_PATH
	}

	err = nil
	for i := 0; i < 3; i++ {
		err = w.fetchParams(paramUri, to)
		if err != nil {
			log.Info(errors.As(err, paramUri, to))
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
