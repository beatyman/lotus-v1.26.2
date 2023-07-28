package main

import (
	"context"
	"github.com/gwaylib/errors"
	ufsdk "github.com/ufilesdk-dev/ufile-gosdk"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MimeType = ""
	Jobs     = 20
)

// upload file from filesystem to us3 oss cluster
func uploadToUfile(ctx context.Context, FilePath, KeyName string) error {
	log.Infof("uploadToUfile start filepath :%+v,keyname: %+v", FilePath, KeyName)
	defer log.Infof("uploadToUfile finish filepath :%+v,keyname: %+v", FilePath, KeyName)
	configPath := strings.TrimSpace(os.Getenv("UFILE_CONFIG"))
	if configPath == "" {
		log.Info("please set UFILE_CONFIG environment variable first!")
		return errors.New("connot find UFILE_CONFIG environment variable")
	}
	if _, err := os.Stat(configPath); err != nil {
		log.Errorf("ufsdk.LoadConfig 配置文件不存在: err: %+v", err.Error())
		return err
	}
	// 加载配置，创建请求
	config, err := ufsdk.LoadConfig(configPath)
	if err != nil {
		log.Errorf("ufsdk.LoadConfig 加载配置文件 err: %+v", err.Error())
		return err
	}
	req, err := ufsdk.NewFileRequest(config, nil)
	if err != nil {
		log.Errorf("ufsdk.NewFileRequest err: %+v", err.Error())
		return err
	}
	// 异步分片上传本地文件
	err = req.AsyncUpload(FilePath, KeyName, MimeType, Jobs)
	if err != nil {
		log.Errorf("ufsdk.AsyncUpload err: %+v", err.Error())
		return err
	}
	log.Infof("文件上传成功: FilePath: %+v ,KeyName: %+v", FilePath, KeyName)
	ok := req.CompareFileEtag(KeyName, FilePath)
	if !ok {
		log.Errorf("ufsdk.CompareFileEtag err: %+v", err.Error())
		return err
	}
	log.Infof("CompareFileEtag 成功: FilePath: %+v ,KeyName: %+v", FilePath, KeyName)
	return nil
}

// download file from local to US3 oss cluste
func downloadFromUfile(ctx context.Context, BigFileKeyName, DownloadFilePath string) error {
	log.Infof("downloadFromUfile start keyname :%+v,filepath: %+v", BigFileKeyName, DownloadFilePath)
	defer log.Infof("downloadFromUfile finish keyname :%+v,filepath: %+v", BigFileKeyName, DownloadFilePath)
	configPath := strings.TrimSpace(os.Getenv("UFILE_CONFIG"))
	if configPath == "" {
		log.Info("please set UFILE_CONFIG environment variable first!")
		return errors.New("connot find UFILE_CONFIG environment variable")
	}
	if _, err := os.Stat(configPath); err != nil {
		log.Errorf("ufsdk.LoadConfig 配置文件不存在: err: %+v", err.Error())
		return err
	}
	// 加载配置，创建请求
	config, err := ufsdk.LoadConfig(configPath)
	if err != nil {
		log.Errorf("ufsdk.LoadConfig 加载配置文件 err: %+v", err.Error())
		return err
	}
	req, err := ufsdk.NewFileRequest(config, nil)
	if err != nil {
		return err
	}
	// 流式下载文件
	log.Infof("流式下载文件: key: %+v ==> to: %+v ", BigFileKeyName, DownloadFilePath)
	file, err := os.OpenFile(DownloadFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		log.Fatalln("创建文件失败，错误信息为：", err.Error())
	}
	err = req.DownloadFile(file, BigFileKeyName)
	if err != nil {
		log.Errorf("下载文件出错，出错信息为：%+v", err.Error())
		return err
	} else {
		log.Infof("下载文件保存到本地文件%s成功：", DownloadFilePath)
	}
	file.Close() //提前关闭文件，防止 etag 计算不准。
	// 检查所保存文件与远程文件etag是否一致
	ok := req.CompareFileEtag(BigFileKeyName, DownloadFilePath)
	if !ok {
		log.Error("文件下载出错，etag 比对不一致。")
	} else {
		log.Info("文件下载成功, etag 一致")
	}
	return nil
}

func (w *worker) uploadToUfile(ctx context.Context, fromPath, toPath string) error {
	stat, err := os.Stat(fromPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(fromPath)
		}
		return err
	}

	log.Infof("upload from: %s, to: %s", fromPath, toPath)
	if stat.IsDir() {
		if err := CopyFile(ctx, fromPath+"/", toPath+"/", NewTransferer(travelFile, uploadToUfile)); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, fromPath, toPath, NewTransferer(travelFile, uploadToUfile)); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) downloadFromUfile(ctx context.Context, fromPath, toPath string) error {
	configPath := strings.TrimSpace(os.Getenv("UFILE_CONFIG"))
	if configPath == "" {
		log.Info("please set UFILE_CONFIG environment variable first!")
		return errors.New("connot find UFILE_CONFIG environment variable")
	}
	if _, err := os.Stat(configPath); err != nil {
		log.Errorf("ufsdk.LoadConfig 配置文件不存在: err: %+v", err.Error())
		return err
	}
	config, err := ufsdk.LoadConfig(configPath)
	if err != nil {
		log.Errorf("ufsdk.LoadConfig : err: %+v", err.Error())
		return err
	}
	req, err := ufsdk.NewFileRequest(config, nil)
	if err != nil {
		log.Errorf("ufsdk.NewFileRequest : err: %+v", err.Error())
		return err
	}
	list, err := req.PrefixFileList("Prefix", "", 1000)
	if err != nil {
		log.Errorf("req.PrefixFileList : err: %+v", err.Error())
		return err
	}
	log.Infof("PrefixFileList: %+v", list)
	needDownloads := make([]string, 0)
	for _, key := range list.DataSet {
		needDownloads = append(needDownloads, key.FileName)
	}
	if len(needDownloads) > 0 {
		log.Infof("download %+v  to  %+v ", needDownloads, toPath)
		tCtx, cancel := context.WithTimeout(ctx, time.Hour)
		if len(needDownloads) == 1 {
			if err := downloadFromUfile(tCtx, needDownloads[0], toPath); err != nil {
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
				if err := downloadFromUfile(tCtx, need, dstPath); err != nil {
					cancel()
					return errors.As(err, w.workerCfg.IP)
				}
			}
		}
		cancel()
	}
	log.Infof("download from: %s, to: %s", fromPath, toPath)
	return nil
}
