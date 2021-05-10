package main

import (
	"encoding/binary"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
)

type connFile struct {
	lk         sync.Mutex
	lastActive time.Time
	file       *os.File

	user       string
	auth       string
	remotePath string
}

var (
	openFiles   = map[string]*connFile{}
	openFilesLk = sync.Mutex{}
)

func closeConnFile(id string) error {
	log.Debugf("closeConnFile:%s", id)
	openFilesLk.Lock()
	defer openFilesLk.Unlock()
	f, ok := openFiles[id]
	if !ok {
		return nil
	}
	delete(openFiles, id)
	if err := DeleteSessionFile(id); err != nil {
		return errors.As(err, f.remotePath)
	}
	if err := f.file.Close(); err != nil {
		return errors.As(err, f.remotePath)
	}
	return nil
}
func putConnFile(id string, f *connFile) {
	log.Debugf("putConnFile:%s,%s", id, f.remotePath)
	openFilesLk.Lock()
	defer openFilesLk.Unlock()
	openFiles[id] = f
}
func getConnFile(id string) (*connFile, error) {
	openFilesLk.Lock()
	defer openFilesLk.Unlock()
	f, ok := openFiles[id]
	if ok {
		return f, nil
	}

	// Try restore from session db because maybe it has been gc.
	user, auth, path, err := GetSessionFile(id)
	if err != nil {
		if errors.ErrNoData.Equal(err) {
			return nil, errors.New("file was closed").As(id)
		}
		return nil, errors.As(err)
	}
	read := GetAuthRO()
	canWrite := false
	if auth != read {
		if !authRW(user, auth, path) {
			return nil, errors.New("auth failed, maybe the auth has changed").As(id)
		}
		canWrite = true
	}
	var file *os.File
	if canWrite {
		to := filepath.Join(_repoFlag, path)
		file, err = os.OpenFile(to, os.O_RDWR, 0644) // TODO: does it need the origin file flag?
		if err != nil {
			return nil, errors.As(err)
		}
	} else {
		file, err = os.Open(filepath.Join(_repoFlag, path))
		if err != nil {
			return nil, errors.As(err)
		}
	}
	cf := &connFile{
		lastActive: time.Now(),
		file:       file,

		remotePath: path,
		auth:       auth,
	}
	openFiles[id] = cf
	return cf, nil
}

func gcConnFile() {
	openFilesLk.Lock()
	defer openFilesLk.Unlock()
	now := time.Now()
	for id, cf := range openFiles {
		cf.lk.Lock()
		if now.Sub(cf.lastActive) < 10*time.Minute {
			cf.lk.Unlock()
			continue
		}
		cf.file.Close()
		cf.lk.Unlock()

		log.Infof("gcConnFile:%s", id)
		delete(openFiles, id)
		// save stack to db
		if err := AddSessionFile(id, cf.user, cf.auth, cf.remotePath); err != nil {
			log.Error(errors.As(err, id, cf.user, cf.remotePath, cf.auth))
		}
	}
}

func FUseFileServer(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.As(err)
	}

	// gc the open file maybe it's dead.
	ticker := time.NewTicker(10 * time.Minute)
	stopTicker := make(chan bool, 1)
	defer func() {
		stopTicker <- true
	}()
	go func() {
		for {
			select {
			case <-stopTicker:
				ticker.Stop()
				return
			case <-ticker.C:
				gcConnFile()
			}
		}
	}()

	// accept the net connection.
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Warn(errors.As(err))
			time.Sleep(3e9)
			continue
		}
		go fuseHandleCommon(conn)
	}
}

// handle for common
func fuseHandleCommon(conn net.Conn) {
	defer conn.Close()
	for {
		// read header
		control, bufLen, err := utils.ReadFUseReqHeader(conn)
		if err != nil {
			switch {
			case utils.ErrEOF.Equal(err), utils.ErrFUseClosed.Equal(err):
				// net has closed, ignore to response the status.
			case utils.ErrFUseProto.Equal(err):
				utils.WriteFUseErrResp(conn, 403, err)
			case utils.ErrFUseParams.Equal(err):
				utils.WriteFUseErrResp(conn, 403, err)
			default:
				utils.WriteFUseErrResp(conn, 500, err)
				log.Warn(errors.As(err))
				return
			}
			return
		}

		// deal logic
		switch control {
		case utils.FUSE_REQ_CONTROL_TEXT:
			err = handleFUseText(conn, bufLen)
		default:
			err = handleFUseFile(conn, control, bufLen)
		}

		// deal error
		if err != nil {
			switch {
			case utils.ErrEOF.Equal(err), utils.ErrFUseClosed.Equal(err):
				// net has closed, ignore to response the status.
			case utils.ErrFUseProto.Equal(err):
				utils.WriteFUseErrResp(conn, 403, err)
			case utils.ErrFUseParams.Equal(err):
				utils.WriteFUseErrResp(conn, 403, err)
			default:
				utils.WriteFUseErrResp(conn, 500, err)
				log.Warn(errors.As(err))
				return
			}
		}
	}
}

func handleFUseText(conn net.Conn, bufLen uint32) error {
	p, err := utils.ReadFUseReqText(conn, bufLen)
	if err != nil {
		return errors.As(err)
	}
	switch p["Method"] {
	case "List":
		// TODO: need auth?
		file, ok := p["Path"]
		if !ok {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("'Path' not found"))
			break
		}

		if !validHttpFilePath(file) {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
			break
		}
		repo := _repoFlag
		path := filepath.Join(repo, file)
		fStat, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				utils.WriteFUseErrResp(conn, 500, errors.As(err))
				break
			}
			utils.WriteFUseErrResp(conn, 404, errors.As(err))
			break
		}
		if !fStat.IsDir() {
			utils.WriteFUseSucResp(conn, 200, []utils.ServerFileStat{
				utils.ServerFileStat{
					FileName:    ".",
					IsDirFile:   false,
					FileSize:    fStat.Size(),
					FileModTime: fStat.ModTime(),
				},
			})
			break
		}
		dirs, err := ioutil.ReadDir(path)
		if err != nil {
			utils.WriteFUseErrResp(conn, 500, errors.As(err))
			break
		}
		result := []utils.ServerFileStat{}
		for _, fs := range dirs {
			size := int64(0)
			if !fs.IsDir() {
				size = fs.Size()
			}
			result = append(result, utils.ServerFileStat{
				FileName:    fs.Name(),
				IsDirFile:   fs.IsDir(),
				FileSize:    size,
				FileModTime: fs.ModTime(),
			})
		}
		utils.WriteFUseSucResp(conn, 200, result)
		break

	case "Stat":
		// TODO: need auth?
		file, ok := p["Path"]
		if !ok {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("'Path' not found"))
			break
		}

		if !validHttpFilePath(file) {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
			break
		}
		repo := _repoFlag
		path := filepath.Join(repo, file)
		fStat, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				utils.WriteFUseErrResp(conn, 500, errors.As(err))
				break
			}
			utils.WriteFUseErrResp(conn, 404, errors.As(err))
			break
		}
		utils.WriteFUseSucResp(conn, 200, &utils.ServerFileStat{
			FileName:    ".",
			IsDirFile:   fStat.IsDir(),
			FileSize:    fStat.Size(),
			FileModTime: fStat.ModTime(),
		})
	case "Cap":
		// implement the df -h
		root := _repoFlag
		fs := syscall.Statfs_t{}
		if err := syscall.Statfs(root, &fs); err != nil {
			utils.WriteFUseErrResp(conn, 500, errors.As(err))
			break
		}
		utils.WriteFUseSucResp(conn, 200, &fs)

		// new connection for open cause it should be lock for the open
	case "Open":
		path, ok := p["Path"]
		if !ok {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("'Path' not found"))
			return nil // open the file failed, close the file thread connection.
		}
		if !validHttpFilePath(path) {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
			return nil // open the file failed, close the file thread connection.
		}
		user, _ := p["Sid"]
		auth, _ := p["Auth"]
		read := GetAuthRO()
		canWrite := false
		if auth != read {
			if !authRW(user, auth, path) {
				utils.WriteFUseErrResp(conn, 401, utils.ErrFUseParams.As("Auth failed"))
				return nil
			}
			canWrite = true
		}

		id := uuid.NewString()
		var file *os.File
		if canWrite {
			flag, err := strconv.ParseInt(p["Flag"], 10, 32)
			if err != nil {
				utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
				return nil // open the file failed, close the file thread connection.
			}
			log.Infof("Auth write %s from %s, id", path, conn.RemoteAddr(), id)
			to := filepath.Join(_repoFlag, path)
			dir := filepath.Dir(to)
			if err := os.MkdirAll(dir, 0755); err != nil {
				utils.WriteFUseErrResp(conn, 500, errors.As(err))
				return nil // open the file failed, close the file thread connection.
			}
			file, err = os.OpenFile(to, int(flag), 0644)
			if err != nil {
				if !os.IsNotExist(err) {
					utils.WriteFUseErrResp(conn, 500, errors.As(err))
				}
				utils.WriteFUseErrResp(conn, 404, errors.As(err))
				return nil // open the file failed, close the file thread connection.
			}
		} else {
			file, err = os.Open(filepath.Join(_repoFlag, path))
			if err != nil {
				if !os.IsNotExist(err) {
					utils.WriteFUseErrResp(conn, 500, errors.As(err))
				}
				utils.WriteFUseErrResp(conn, 404, errors.As(err))
				return nil // open the file failed, close the file thread connection.
			}
		}
		putConnFile(id, &connFile{
			lastActive: time.Now(),
			file:       file,

			user:       user,
			auth:       auth,
			remotePath: path,
		})
		utils.WriteFUseSucResp(conn, 200, map[string]string{
			"Id": id,
		}) // tell the client the open is success.

		// deal the file done, keep the conntion continue for other operator
		return nil
	}
	return nil
}

func handleFUseFile(conn net.Conn, control uint8, bufLen uint32) error {
	// read byte[16]
	oriId, err := uuid.NewRandomFromReader(conn)
	if err != nil {
		return err
	}
	id := oriId.String()

	// not need read the session cache
	switch control {
	case utils.FUSE_REQ_CONTROL_FILE_CLOSE:
		if err := closeConnFile(id); err != nil {
			return errors.As(err, control, bufLen)
		}
		return nil
	}

	// deal file handle.
	cf, err := getConnFile(id)
	if err != nil {
		return errors.As(err, control, bufLen)
	}
	// update last active time.
	cf.lk.Lock()
	cf.lastActive = time.Now()
	cf.lk.Unlock()
	file := cf.file
	switch control {
	case utils.FUSE_REQ_CONTROL_FILE_WRITE:
		if bufLen == 0 {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("read buffer length can not be zero"))
			return nil
		}
		offB := make([]byte, 8) // off
		if _, err := conn.Read(offB); err != nil {
			if utils.ErrEOF.Equal(err) {
				return utils.ErrFUseClosed.As(file, bufLen)
			}
			return errors.As(err)
		}
		off, _ := binary.Varint(offB)
		if _, err := file.Seek(off, 0); err != nil {
			return errors.As(err)
		}
		if _, err := io.CopyN(file, conn, int64(bufLen)); err != nil {
			return errors.As(err)
		}
		return nil

	case utils.FUSE_REQ_CONTROL_FILE_READ:
		if bufLen == 0 {
			utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("read buffer length can not be zero"))
			return nil
		}
		offB := make([]byte, 8) // off
		if _, err := conn.Read(offB); err != nil {
			if utils.ErrEOF.Equal(err) {
				return utils.ErrFUseClosed.As(file, bufLen)
			}
			return errors.As(err, file, bufLen)
		}
		off, _ := binary.Varint(offB)
		if _, err := file.Seek(off, 0); err != nil {
			if utils.ErrEOF.Equal(err) {
				return utils.ErrFUseClosed.As(file, off, bufLen)
			}
			return errors.As(err, file, off, bufLen)
		}

		fileStat, err := file.Stat()
		if err != nil {
			return errors.As(err)
		}
		fileSize := fileStat.Size()
		if (off + int64(bufLen)) > fileSize {
			return utils.ErrFUseParams.As("off + buffLen more than file.Size")

		}

		if err := utils.WriteFUseRespHeader(conn, utils.FUSE_RESP_CONTROL_FILE_TRANSFER, bufLen); err != nil {
			return errors.As(err)
		}
		if _, err := io.CopyN(conn, file, int64(bufLen)); err != nil {
			if io.EOF != err {
				return errors.As(err, file, off, bufLen)
			}
			// reach end, nothing to do.
		}
		return nil

	case utils.FUSE_REQ_CONTROL_FILE_TRUNC:
		sizeB := make([]byte, 8)
		if _, err := conn.Read(sizeB); err != nil {
			if utils.ErrEOF.Equal(err) {
				return utils.ErrFUseClosed.As(file, bufLen)
			}
			return errors.As(err)
		}
		size, _ := binary.Varint(sizeB)
		if err := file.Truncate(size); err != nil {
			return errors.As(err)
		}
		utils.WriteFUseSucResp(conn, 200, nil)
		return nil

	case utils.FUSE_REQ_CONTROL_FILE_STAT:
		fStat, err := file.Stat()
		if err != nil {
			return errors.As(err)
		}
		utils.WriteFUseSucResp(conn, 200, &utils.ServerFileStat{
			FileName:    ".",
			IsDirFile:   fStat.IsDir(),
			FileSize:    fStat.Size(),
			FileModTime: fStat.ModTime(),
		})
		return nil
	}
	return utils.ErrFUseProto.As("unknow control code", control)
}
