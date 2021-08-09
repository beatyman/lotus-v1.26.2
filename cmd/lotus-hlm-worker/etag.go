package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"github.com/gwaylib/errors"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/api.v7/kodo"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BLOCK_BITS = 22 // Indicate that the blocksize is 4M
	BLOCK_SIZE = 1 << BLOCK_BITS
)

func BlockCount(fsize int64) int {
	return int((fsize + (BLOCK_SIZE - 1)) >> BLOCK_BITS)
}

func CalSha1(b []byte, r io.Reader) ([]byte, error) {

	h := sha1.New()
	_, err := io.Copy(h, r)
	if err != nil {
		return nil, err
	}
	return h.Sum(b), nil
}

func ComputeEtagLocal(filename string) (etag string, err error) {

	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return
	}

	fsize := fi.Size()
	blockCnt := BlockCount(fsize)
	sha1Buf := make([]byte, 0, 21)

	if blockCnt <= 1 { // file size <= 4M
		sha1Buf = append(sha1Buf, 0x16)
		sha1Buf, err = CalSha1(sha1Buf, f)
		if err != nil {
			return
		}
	} else { // file size > 4M
		sha1Buf = append(sha1Buf, 0x96)
		sha1BlockBuf := make([]byte, 0, blockCnt*20)
		for i := 0; i < blockCnt; i++ {
			body := io.LimitReader(f, BLOCK_SIZE)
			sha1BlockBuf, err = CalSha1(sha1BlockBuf, body)
			if err != nil {
				return
			}
		}
		sha1Buf, _ = CalSha1(sha1Buf, bytes.NewReader(sha1BlockBuf))
	}
	etag = base64.URLEncoding.EncodeToString(sha1Buf)
	return
}
func GetEtagFromServer(ctx context.Context, key string) (string, error) {
	up := os.Getenv("US3")
	if up == "" {
		return "", errors.New("US3 Config Not Found")
	}
	config, err := operation.Load(up)
	if err != nil {
		return "", err
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
		return "", err
	}
	entry, err := bucket.Stat(ctx, key)
	if err != nil {
		return "", err
	}
	return entry.Hash, nil
}

//临时用list接口替代,list接口不是很稳定
func GetEtagFromServer2(ctx context.Context, key string) (string, error) {
	prefix, err := filepath.Abs(filepath.Dir(key))
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("prefix: %+v ,key: %+v ", prefix, key)
	up := os.Getenv("US3")
	if up == "" {
		return "", errors.New("US3 Config Not Found")
	}
	config, err := operation.Load(up)
	if err != nil {
		return "", err
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
		return "", err
	}
	var files []string
	var etags []string
	marker := ""
	hash := ""
	for {
		r, _, out, err := bucket.List(nil, prefix, "", marker, 1000)
		if err != nil && err != io.EOF {
			time.Sleep(time.Second)
			log.Errorf("get file %+v etag err: %+v", key, err.Error())
			continue
		}
		for _, v := range r {
			if strings.EqualFold(v.Key, key) {
				hash = v.Hash
			}
			files = append(files, v.Key)
			etags = append(etags, v.Hash)
		}
		if out == "" {
			break
		}
		marker = out
	}
	if hash != "" {
		return hash, nil
	}
	log.Infof(" files: %+v , etags:%+v ",files,etags)
	return "", errors.New(fmt.Sprintf("file: %+v get etag error",key))
}
