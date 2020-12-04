package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dchest/blake2b"
	paramfetch "github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/gwaylib/errors"
)

const (
	PARAMS_PATH = "/file/filecoin-proof-parameters"
)

var (
	ErrChecksum  = errors.New("checksum failed")
	ErrMinerDone = errors.New("miner download failed")
)

type paramFile struct {
	Cid        string         `json:"cid"`
	Digest     string         `json:"digest"`
	SectorSize abi.SectorSize `json:"sector_size"`
}

var checked = map[string]bool{}
var checkedLk sync.Mutex

func addChecked(file string) {
	checkedLk.Lock()
	checked[file] = true
	checkedLk.Unlock()
}
func delChecked(file string) {
	log.Warnf("Parameter file sum failed, remove:%s", file)
	if err := os.RemoveAll(file); err != nil {
		log.Error(errors.As(err))
	}
	checkedLk.Lock()
	delete(checked, file)
	checkedLk.Unlock()
}

func sumFile(path string, info paramFile) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := blake2b.New512()
	if _, err := io.Copy(h, f); err != nil {
		return errors.As(err)
	}

	sum := h.Sum(nil)
	strSum := hex.EncodeToString(sum[:16])
	if strSum != info.Digest {
		return ErrChecksum.As(path)
	}
	return nil
}

func checkFile(path string, info paramFile, needCheckSum bool) error {
	log.Infof("checking: %s", path)
	if os.Getenv("TRUST_PARAMS") == "1" {
		log.Warn("Assuming parameter files are ok. DO NOT USE IN PRODUCTION")
		return nil
	}

	checkedLk.Lock()
	_, ok := checked[path]
	checkedLk.Unlock()
	if ok {
		log.Infof("from cached: %s", path)
		return nil
	}
	// ignore blk2b checking
	_, err := os.Stat(path)
	if err != nil {
		return errors.As(err)
	}

	done := make(chan error, 1)
	go func(p string, checksum bool) {
		err := sumFile(p, info)
		if err != nil {
			delChecked(path)
			if !checksum {
				// checksum has ignored, exit the worker to make the worker down
				time.Sleep(3e9) // waiting log output
				os.Exit(1)      // TODO: this cann't interrupt the connection
				return
			}
		}
		log.Infof("checksum %s done", path)
		done <- err
	}(path, needCheckSum)
	if needCheckSum {
		err := <-done
		if err != nil {
			return errors.As(err)
		}
	} else {
		log.Warnf("Ingore checksum parameters file: %s", path)
	}
	addChecked(path)
	return nil
}

func (w *worker) CheckParams(ctx context.Context, endpoint, paramsDir string, ssize abi.SectorSize) error {
	w.paramsLock.Lock()
	defer w.paramsLock.Unlock()
	log.Infof("CheckParams:%s,%s,%s", endpoint, paramsDir, ssize)

	for {
		select {
		case <-ctx.Done():
			return errors.New("ctx done")
		default:
		}

		err := w.checkLocalParams(ctx, ssize, endpoint, paramsDir)
		if err == nil {
			return nil
		}

		// try the next server
		if errors.ErrNoData.Equal(err) {
			continue
		}

		// get from the offical source
		if ErrMinerDone.Equal(err) {
			if err := paramfetch.GetParams(ctx, build.ParametersJSON(), uint64(ssize)); err != nil {
				return errors.As(err)
			}
			return nil
		}

		// other error go to try the next local server
		time.Sleep(10e9)
	}
}

func (w *worker) checkLocalParams(ctx context.Context, ssize abi.SectorSize, endpoint, paramsDir string) error {
	if err := os.MkdirAll(paramsDir, 0755); err != nil {
		return errors.As(err)
	}
	paramBytes := build.ParametersJSON()
	var params map[string]paramFile
	if err := json.Unmarshal(paramBytes, &params); err != nil {
		return errors.As(err)
	}

recheck:
	for name, info := range params {
		if ssize != info.SectorSize && strings.HasSuffix(name, ".params") {
			continue
		}
		fPath := filepath.Join(paramsDir, name)
		if _, err := os.Stat(fPath); err != nil {
			w.needCheckSum = true
			log.Warnf("miss %s, checksum all parameter files", fPath)
			break
		}
	}

	for name, info := range params {
		if ssize != info.SectorSize && strings.HasSuffix(name, ".params") {
			continue
		}
		fPath := filepath.Join(paramsDir, name)
		if err := checkFile(fPath, info, w.needCheckSum); err != nil {
			log.Info(errors.As(err))

			if !w.needCheckSum {
				w.needCheckSum = true
				goto recheck
			}

			// try to fetch the params
			if err := w.fetchParams(ctx, endpoint, paramsDir, name); err != nil {
				return errors.As(err)
			}
			// checksum again
			if err := checkFile(fPath, info, true); err != nil {
				if ErrChecksum.Equal(err) {
					if err := os.RemoveAll(fPath); err != nil {
						return errors.As(err)
					}
				}
				return errors.As(err)
			}
			// pass download
		}
	}
	return nil
}

var skipDownloadSrv = []string{""}

func (w *worker) fetchParams(ctx context.Context, endpoint, paramsDir, fileName string) error {
	napi, err := GetNodeApi()
	if err != nil {
		return err
	}
	paramUri := ""
	dlWorkerUsed := false
	skipDownloadSrv[0] = w.workerCfg.ID
	// try download from worker
	dlWorker, err := napi.WorkerPreConn(ctx, skipDownloadSrv)
	if err != nil {
		if !errors.ErrNoData.Equal(err) {
			return errors.As(err)
		}
		// pass, using miner's
	} else {
		// mark the server has used
		dlWorkerUsed = true
		paramUri = "http://" + dlWorker.SvcUri + PARAMS_PATH
		skipDownloadSrv = append(skipDownloadSrv, dlWorker.ID)
	}
	defer func() {
		if !dlWorkerUsed {
			return
		}

		// return preconn
		if err := napi.WorkerAddConn(ctx, dlWorker.ID, -1); err != nil {
			log.Warn(err)
		}
	}()

	// try download from miner
	if len(paramUri) == 0 {
		minerConns, err := napi.WorkerMinerConn(ctx)
		if err != nil {
			return errors.As(err)
		}
		// no worker online, get from miner
		if minerConns > 1 {
			return errors.New("miner download connections full")
		}
		paramUri = "http://" + endpoint + PARAMS_PATH
	}

	// get the params from local net
	err = nil
	from := fmt.Sprintf("%s/%s", paramUri, fileName)
	to := filepath.Join(paramsDir, fileName)
	for i := 0; i < 3; i++ {
		err = w.fetchRemoteFile(from, to)
		if err != nil {
			continue
		}
		return nil
	}

	// the miner has tried, return then try end.
	if err != nil && !dlWorkerUsed {
		return ErrMinerDone
	}
	return err
}
