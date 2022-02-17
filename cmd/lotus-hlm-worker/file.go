package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gwaylib/errors"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
)

const (
	append_file_new       = 0
	append_file_continue  = 1
	append_file_completed = 2
)

func checksumFile(aFile, bFile *os.File) (int, error) {
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	// checksum all data
	ah := sha1.New()
	if _, err := io.Copy(ah, aFile); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	aSum := ah.Sum(nil)

	bh := sha1.New()
	if _, err := io.Copy(bh, bFile); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	bSum := bh.Sum(nil)

	if !bytes.Equal(aSum, bSum) {
		return append_file_new, nil
	}

	return append_file_completed, nil
}
func canAppendFile(aFile, bFile *os.File, aStat, bStat os.FileInfo) (int, error) {
	checksumSize := int64(32 * 1024)
	// for small size, just do rewrite.
	aSize := aStat.Size()
	bSize := bStat.Size()
	if bSize < checksumSize {
		return append_file_new, nil
	}
	if bSize > aSize {
		return append_file_new, nil
	}

	// checksum the end
	aData := make([]byte, checksumSize)
	bData := make([]byte, checksumSize)
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	if _, err := aFile.ReadAt(aData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.ReadAt(bData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if !bytes.Equal(aData, bData) {
		return append_file_new, nil
	}
	if aSize > bSize {
		return append_file_continue, nil
	}

	return append_file_completed, nil
}

func travelFileEmpty(path string) (os.FileInfo, []string, error) {
	return nil, []string{path}, nil
}

func travelFile(path string) (os.FileInfo, []string, error) {
	fStat, err := os.Lstat(path)
	if err != nil {
		return nil, nil, errors.As(err, path)
	}
	if !fStat.IsDir() {
		return nil, []string{path}, nil
	}
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, nil, errors.As(err)
	}
	result := []string{}
	for _, fs := range dirs {
		filePath := filepath.Join(path, fs.Name())
		if !fs.IsDir() {
			result = append(result, filePath)
			continue
		}
		_, nextFiles, err := travelFile(filePath)
		if err != nil {
			return nil, nil, errors.As(err, filePath)
		}
		result = append(result, nextFiles...)
	}
	return fStat, result, nil
}

func copyFile(ctx context.Context, from, to string) error {
	if from == to {
		return errors.New("Same file").As(from, to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return errors.As(err, to)
	}
	fromFile, err := os.Open(from)
	if err != nil {
		return errors.As(err, from)
	}
	defer fromFile.Close()
	fromStat, err := fromFile.Stat()
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(to)
		}
		return errors.As(err, to)
	}

	// TODO: make chtime
	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE, fromStat.Mode())
	if err != nil {
		return errors.As(err, to, uint64(fromStat.Mode()))
	}
	defer toFile.Close()
	toStat, err := toFile.Stat()
	if err != nil {
		return errors.As(err, to)
	}

	// checking continue
	stats, err := canAppendFile(fromFile, toFile, fromStat, toStat)
	if err != nil {
		return errors.As(err)
	}
	switch stats {
	case append_file_completed:
		// has done
		fmt.Printf("%s ======= completed\n", to)
		return nil

	case append_file_continue:
		appendPos := int64(toStat.Size() - 1)
		if appendPos < 0 {
			appendPos = 0
			fmt.Printf("%s ====== new \n", to)
		} else {
			fmt.Printf("%s ====== continue: %d\n", to, appendPos)
		}
		if _, err := fromFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
	default:
		fmt.Printf("%s ====== new \n", to)
		// TODO: allow truncate, current need to delete the files by manully.
		if err := toFile.Truncate(0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
		if _, err := fromFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
	}

	errBuff := make(chan error, 1)
	interrupt := false
	iLock := sync.Mutex{}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			iLock.Lock()
			if interrupt {
				iLock.Unlock()
				return
			}
			iLock.Unlock()

			nr, er := fromFile.Read(buf)
			if nr > 0 {
				nw, ew := toFile.Write(buf[0:nr])
				if ew != nil {
					errBuff <- errors.As(ew)
					return
				}
				if nr != nw {
					errBuff <- errors.As(io.ErrShortWrite)
					return
				}
			}
			if er != nil {
				errBuff <- errors.As(er)
				return
			}
		}
	}()
	select {
	case err := <-errBuff:
		if !errors.Equal(err, io.EOF) {
			return errors.As(err)
		}
		stats, err := checksumFile(fromFile, toFile)
		if err != nil {
			return errors.As(err)
		}
		if stats == append_file_completed {
			return nil
		}
		// TODO: allow truncate, current need to delete the files by manully.
		//if err := toFile.Truncate(0); err != nil {
		//	return errors.As(err, toFile)
		//}
		//if _, err := toFile.Seek(0, 0); err != nil {
		//	return errors.As(err, toFile)
		//}
		return errors.New("finalize has completed, but checksum failed.").As(stats, from, to, fromStat.Size(), toStat.Size())
	case <-ctx.Done():
		iLock.Lock()
		interrupt = true
		iLock.Unlock()
		return ctx.Err()
	}
}

type Transferer struct {
	travel func(path string) (os.FileInfo, []string, error)
	copy   func(ctx context.Context, from, to string) error
}

func NewTransferer(t func(path string) (os.FileInfo, []string, error), h func(ctx context.Context, from, to string) error) *Transferer {
	tran := &Transferer{
		travel: t,
		copy:   h,
	}
	return tran
}

func CopyFile(ctx context.Context, from, to string, t ...*Transferer) error {
	copyfunc := &Transferer{copy: copyFile, travel: travelFile}
	if len(t) != 0 {
		copyfunc = t[0]
	}
	_, source, err := copyfunc.travel(from)
	if err != nil {
		return errors.As(err)
	}

	for _, src := range source {
		toFile := strings.Replace(src, from, to, 1)
		tCtx, cancel := context.WithTimeout(ctx, time.Hour)
		log.Infof("src: %+v ,toFile : %+v", src, toFile)
		if err := copyfunc.copy(tCtx, src, toFile); err != nil {
			cancel()
			log.Warn(errors.As(err))

			// try again
			tCtx, cancel = context.WithTimeout(ctx, time.Hour)
			if err := copyfunc.copy(tCtx, src, toFile); err != nil {
				cancel()
				return errors.As(err)
			}
		}
		cancel()
	}
	return nil
}

// upload file from filesystem to us3 oss cluster
func uploadToOSS(ctx context.Context, from, to string) error {
	up := os.Getenv("US3")
	if up == "" {
		log.Info("please set US3 environment variable first!")
		return errors.New("connot find US3 environment variable")
	}
	conf2, err := operation.Load(up)
	if err != nil {
		log.Error("load config error", err)
		return errors.As(err)
	}
	uploader := operation.NewUploaderV2()
	log.Infof("start upload :  %s to %s ", from, to)
	timeStart := time.Now()
	err = uploader.Upload(from, to)
	if err != nil {
		return errors.As(err)
	}
	timeEnd := time.Now()
	log.Infof("file  upload  %s to %s ,start :%+v, end: %+v, last:%+v ", from, to, timeStart, timeEnd, timeEnd.Sub(timeStart))
	etagLocal, err := ComputeEtagLocal(from)
	if err != nil {
		return errors.As(err)
	}
	log.Infof("etagLocal: %+v", etagLocal)
	etagRemote, err := GetEtagFromServer2(ctx, to[1:])
	if err != nil {
		return errors.As(err)
	}
	if !strings.EqualFold(etagLocal, etagRemote) {
		return errors.New(fmt.Sprintf("file %+v etag not match: local: %+v ,remote: %+v", from, etagLocal, etagRemote))
	}
	log.Infof("finish upload :  %s to %s ,err: %+v  ", from, to, err)
	if conf2.Delete {
		if err == nil {
			os.Remove(from)
		}
	}
	return err
}

// download file from local to US3 oss cluste
func downloadFromOSS(ctx context.Context, from, to string) error {
	log.Infof("from = %+v, to = %+v,", from, to)
	up := os.Getenv("US3")
	if up == "" {
		fmt.Println("please set US3 environment variable first!")
		return errors.New("connot find US3 environment variable")
	}
	conf2, err := operation.Load(up)
	if err != nil {
		log.Error("load config error", err)
		return errors.As(err)
	}
	downloader := operation.NewDownloader(conf2)
	_, err = downloader.DownloadFile(from, to)
	if err != nil {
		fmt.Printf("downloadFileFromUS3 failed download file from %s to %s err %v\n", from, to, err)
	}

	return err
}

func lastTreePaths(cacheDir string) []string {
	var ret []string
	paths, err := ioutil.ReadDir(cacheDir)
	fmt.Println(err)
	if err != nil {
		return []string{}
	}
	fmt.Println(paths)
	for _, v := range paths {
		if !v.IsDir() {
			if strings.Contains(v.Name(), "tree-r-last") ||
				v.Name() == "p_aux" || v.Name() == "t_aux" {
				ret = append(ret, path.Join(cacheDir, v.Name()))
			}
		}
	}
	return ret
}

func submitQ(paths storiface.SectorPaths, sector abi.SectorID) error {
	fmt.Printf("submit path %#v sector %#v\n", paths, sector)
	cache := paths.Cache
	seal := paths.Sealed

	pathList := lastTreePaths(cache)
	pathList = append(pathList, seal, paths.Unsealed)
	var reqs []*req
	for _, path := range pathList {
		fmt.Println("path ", path)
		reqs = append(reqs, newReq(path))
	}
	return submitPaths(reqs)
}

func submitPaths(paths []*req) error {
	up := os.Getenv("US3")
	if up == "" {
		return nil
	}
	uploader := operation.NewUploaderV2()
	for _, v := range paths {
		fmt.Println(*v)
		err := uploader.Upload(v.Path, v.Path)
		log.Infof("US3 : submit path=%v err=%v\n", v.Path, err)
		if err != nil {
			return err
		}

		if !strings.Contains(v.Path, ".genesis-sectors") {
			//上传完成后自动清理文件逻辑
			//os.Remove(v.Path)
		}

	}
	return nil
}

type req struct {
	Path string `json:"path"`
}

func newReq(s string) *req {
	return &req{
		Path: s,
	}
}
