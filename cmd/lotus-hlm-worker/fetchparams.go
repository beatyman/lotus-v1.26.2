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
	// paramfetch "github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/gwaylib/errors"
)

const (
	PARAMS_PATH = "/file/filecoin-proof-parameters"
)

var (
	ErrChecksum = errors.New("checksum failed")
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
	log.Warnf("Parameter file %s sum failed", file)
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

func checkFile(path string, info paramFile, ignoreSum bool) error {
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
	// ignore blk2b checking
	_, err := os.Stat(path)
	if err != nil {
		return errors.As(err)
	}

	done := make(chan error, 1)
	go func(p string, ignSum bool) {
		log.Infof("checksum %s", p)
		err := sumFile(p, info)
		if err != nil {
			delChecked(path)
			if ignSum {
				// checksum has ignore, exit the worker to make worker down
				os.Exit(1)
			}
		}
		done <- err
	}(path, ignoreSum)
	if !ignoreSum {
		err := <-done
		if err != nil {
			return errors.As(err)
		}
		log.Infof("checksum %s done", path)
	} else {
		log.Warnf("Ingore checksum parameters file: %s", path)
	}
	addChecked(path)
	return nil
}

func (w *worker) CheckParams(ctx context.Context, endpoint, paramsDir string, ssize abi.SectorSize) error {
	w.paramsLock.Lock()
	defer w.paramsLock.Unlock()

	//// for origin params
	//if err := paramfetch.GetParams(ctx, build.ParametersJSON(), ssize); err != nil {
	//	return errors.As(err)
	//}

	for {
		if err := w.checkParams(ctx, ssize, endpoint, paramsDir); err != nil {
			log.Info(errors.As(err))
			time.Sleep(10e9)
			continue
		}
		return nil
	}
}

func (w *worker) checkParams(ctx context.Context, ssize abi.SectorSize, endpoint, paramsDir string) error {
	if err := os.MkdirAll(paramsDir, 0755); err != nil {
		return errors.As(err)
	}
	paramBytes := build.ParametersJSON()
	var params map[string]paramFile
	if err := json.Unmarshal(paramBytes, &params); err != nil {
		return errors.As(err)
	}
	for name, info := range params {
		if ssize != info.SectorSize && strings.HasSuffix(name, ".params") {
			continue
		}
		fPath := filepath.Join(paramsDir, name)
		if err := checkFile(fPath, info, true); err != nil {
			log.Info(errors.As(err))
			if err := w.fetchParams(ctx, endpoint, paramsDir, name); err != nil {
				return errors.As(err)
			}
			// checksum again
			if err := checkFile(fPath, info, false); err != nil {
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

func (w *worker) fetchParams(ctx context.Context, endpoint, paramsDir, fileName string) error {
	napi, err := GetNodeApi()
	if err != nil {
		return err
	}
	paramUri := ""
	dlWorkerUsed := false
	// try download from worker
	dlWorker, err := napi.WorkerPreConn(ctx)
	if err != nil {
		if !errors.ErrNoData.Equal(err) {
			return errors.As(err)
		}
		// pass, using miner's
	} else {
		if dlWorker.SvcConn < 2 {
			dlWorkerUsed = true
			paramUri = "http://" + dlWorker.SvcUri + PARAMS_PATH
		} else {
			// return preconn
			if err := napi.WorkerAddConn(ctx, dlWorker.ID, -1); err != nil {
				log.Warn(err)
			}
			// worker all busy, using miner's
		}
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
	return err
}
