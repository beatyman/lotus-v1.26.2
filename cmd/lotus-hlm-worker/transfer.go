package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/api.v7/kodo"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/filecoin-project/lotus/lib/fileserver"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
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

func (w *worker) fetchRemote(serverUri, sectorID, toRepo string, typ ffiwrapper.WorkerTaskType) error {
	var err error
	for i := 0; i < 3; i++ {
		err = w.tryFetchRemote(serverUri, sectorID, toRepo, typ)
		if err != nil {
			log.Warn(errors.As(err, i, serverUri, sectorID, typ))
			continue
		}
		return nil
	}
	return err
}
func (w *worker) tryFetchRemote(serverUri string, sectorID, toRepo string, typ ffiwrapper.WorkerTaskType) error {
	switch typ {
	case ffiwrapper.WorkerPreCommit1:
		// fetch unsealed
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/unsealed/%s", serverUri, sectorID),
			filepath.Join(toRepo, "unsealed", sectorID),
		); err != nil {
			return errors.As(err, typ)
		}

	default:
		// fetch unsealed, prepare for p2 or c2 failed to p1, p1 need the unsealed.
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/unsealed/%s", serverUri, sectorID),
			filepath.Join(toRepo, "unsealed", sectorID),
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
		if err := os.MkdirAll(filepath.Join(toRepo, "cache", sectorID), 0755); err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
		for _, file := range cacheDir.Files {
			if err := w.fetchRemoteFile(
				fmt.Sprintf("%s/file/storage/cache/%s/%s", serverUri, sectorID, file.Value),
				filepath.Join(toRepo, "cache", sectorID, file.Value),
			); err != nil {
				return errors.As(err, serverUri, sectorID, typ)
			}
		}

		// fetch sealed
		if err := w.fetchRemoteFile(
			fmt.Sprintf("%s/file/storage/sealed/%s", serverUri, sectorID),
			filepath.Join(toRepo, "sealed", sectorID),
		); err != nil {
			return errors.As(err, serverUri, sectorID, typ)
		}
	}

	return nil
}

func (w *worker) rsync(ctx context.Context, fromPath, toPath string) error {
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

func (w *worker) upload(ctx context.Context, fromPath, toPath string) error {
	stat, err := os.Stat(fromPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(fromPath)
		}
		return err
	}

	log.Infof("upload from: %s, to: %s", fromPath, toPath)
	if stat.IsDir() {
		if err := CopyFile(ctx, fromPath+"/", toPath+"/", NewTransferer(travelFile, uploadToOSS)); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, fromPath, toPath, NewTransferer(travelFile, uploadToOSS)); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) download(ctx context.Context, fromPath, toPath string) error {
	if len(fromPath) > 1 {
		needDownloads, err := GetDownloadFilesKeys(ctx, fromPath[1:])
		if err != nil {
			return err
		}
		if len(needDownloads) > 0 {
			log.Infof("download %+v  to  %+v ", needDownloads, toPath)
			tCtx, cancel := context.WithTimeout(ctx, time.Hour)
			if len(needDownloads) == 1 {
				if err := downloadFromOSS(tCtx, needDownloads[0], toPath); err != nil {
					cancel()
					return errors.As(err, w.workerCfg.IP)
				}
			} else {
				if err := os.RemoveAll(toPath); err != nil {
					log.Warn(err)
				}
				if err := os.MkdirAll(toPath, 0755); err != nil {
					cancel()
					return errors.As(err, w.workerCfg.IP)
				}
				for _, need := range needDownloads {
					dstPath := filepath.Join(toPath, filepath.Base(need))
					if err := downloadFromOSS(tCtx, need, dstPath); err != nil {
						cancel()
						return errors.As(err, w.workerCfg.IP)
					}
				}
			}
			cancel()
		}
	}
	log.Infof("download from: %s, to: %s", fromPath, toPath)
	return nil
}

// 遍历对象存储下载的key
func GetDownloadFilesKeys(ctx context.Context, key string) ([]string, error) {
	prefix := path.Dir(key)
	log.Infof("prefix: %+v ,key: %+v ", prefix, key)
	up := os.Getenv("US3")
	if up == "" {
		return []string{}, errors.New("US3 Config Not Found")
	}
	config, err := operation.Load(up)
	if err != nil {
		return []string{}, err
	}
	cfg := kodo.Config{
		AccessKey: config.Ak,
		SecretKey: config.Sk,
		RSHost:    config.RsHosts[0],  //stat 主要用这个
		RSFHost:   config.RsfHosts[0], //列表主要靠这个配置
		UpHosts:   config.UpHosts,
	}
	client := kodo.NewWithoutZone(&cfg)
	bucket, err := client.BucketWithSafe(config.Bucket)
	if err != nil {
		return []string{}, err
	}
	marker := ""
	result := make([]string, 0)
	for {
		r, _, out, err := bucket.List(ctx, prefix, "", marker, 1000)
		if err != nil && err != io.EOF {
			time.Sleep(time.Second)
			log.Errorf("get file %+v etag err: %+v", key, err.Error())
			continue
		}
		for _, v := range r {
			result = append(result, v.Key)
		}
		if out == "" {
			break
		}
		marker = out
	}
	return result, nil
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