package main

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func FUseFileServer(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.As(err)
	}
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
		p, err := utils.ReadFUseTextReq(conn)
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
				return // open the file failed, close the file thread connection.
			}
			if !validHttpFilePath(path) {
				utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
				return // open the file failed, close the file thread connection.
			}
			sid, _ := p["Sid"]
			auth, _ := p["Auth"]
			read := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"read")))
			write := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"write")))
			canWrite := false
			var file *os.File
			switch auth {
			case read:
				// pass
			case write:
				canWrite = true
				// pass
			default:
				if !_handler.VerifyToken(sid, auth) {
					utils.WriteFUseErrResp(conn, 401, utils.ErrFUseParams.As("Auth failed"))
					return // open the file failed, close the file thread connection.
				}
				if !strings.Contains(path, sid) {
					utils.WriteFUseErrResp(conn, 401, utils.ErrFUseParams.As("Auth path failed"))
					return // open the file failed, close the file thread connection.
				}
				canWrite = true
			}
			if canWrite {
				flag, err := strconv.ParseInt(p["Flag"], 10, 32)
				if err != nil {
					utils.WriteFUseErrResp(conn, 403, utils.ErrFUseParams.As("invalid path"))
					return // open the file failed, close the file thread connection.
				}
				log.Infof("Auth write %s from %s", path, conn.RemoteAddr())
				to := filepath.Join(_repoFlag, path)
				dir := filepath.Dir(to)
				if err := os.MkdirAll(dir, 0755); err != nil {
					utils.WriteFUseErrResp(conn, 500, errors.As(err))
					return // open the file failed, close the file thread connection.
				}
				file, err = os.OpenFile(to, int(flag), 0644)
				if err != nil {
					if !os.IsNotExist(err) {
						utils.WriteFUseErrResp(conn, 500, errors.As(err))
					}
					utils.WriteFUseErrResp(conn, 404, errors.As(err))
					return // open the file failed, close the file thread connection.
				}
			} else {
				file, err = os.Open(filepath.Join(_repoFlag, path))
				if err != nil {
					if !os.IsNotExist(err) {
						utils.WriteFUseErrResp(conn, 500, errors.As(err))
					}
					utils.WriteFUseErrResp(conn, 404, errors.As(err))
					return // open the file failed, close the file thread connection.
				}
			}
			utils.WriteFUseSucResp(conn, 200, nil) // tell the client the open is success, and waitting the next command.
			if err := handleFUseFile(conn, file); err != nil {
				// open the file failed, close the file thread connection.
				switch {
				case utils.ErrEOF.Equal(err), utils.ErrFUseClosed.Equal(err):
					log.Info(errors.As(err))
				default:
					log.Warn(errors.As(err))
					utils.WriteFUseErrResp(conn, 500, errors.As(err))
				}
				return
			}
			// deal the file done, keep the conntion continue for other operator
			continue
		}
	}
}

func handleFUseFile(conn net.Conn, file *os.File) error {
	defer file.Close()
	fileStat, err := file.Stat()
	if err != nil {
		return errors.As(err)
	}
	for {
		control, bufLen, err := utils.ReadFUseReqHeader(conn)
		if err != nil {
			return errors.As(err)
		}

		switch control {
		case utils.FUSE_REQ_CONTROL_TEXT:
			// should not reach here, but it has happend, return the debug message.
			params, err := utils.ReadFUseReqText(conn, bufLen)
			if err != nil {
				return errors.As(err, control)
			}
			return utils.ErrFUseProto.As(params)

		case utils.FUSE_REQ_CONTROL_FILE_WRITE:
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
			if bufLen == 0 {
				if _, err := io.Copy(file, conn); err != nil {
					return errors.As(err)
				}
			} else {
				if _, err := io.CopyN(file, conn, int64(bufLen)); err != nil {
					return errors.As(err)
				}
			}
		case utils.FUSE_REQ_CONTROL_FILE_READ:
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
			fileSize := fileStat.Size()
			if (off + int64(bufLen)) > fileSize {
				return utils.ErrFUseParams.As("off + buffLen > file.Size")

			}
			if _, err := conn.Write([]byte{utils.FUSE_RESP_CONTROL_FILE_TRANSFER}); err != nil {
				if utils.ErrEOF.Equal(err) {
					return utils.ErrFUseClosed.As(file, off, bufLen)
				}
				return errors.As(err, file, off, bufLen)
			}
			if bufLen == 0 {
				if _, err := io.Copy(conn, file); err != nil {
					if io.EOF != err {
						return errors.As(err, file, off, bufLen)
					}
					// reach end, nothing to do.
				}
			} else {
				if _, err := io.CopyN(conn, file, int64(bufLen)); err != nil {
					if io.EOF != err {
						return errors.As(err, file, off, bufLen)
					}
					// reach end, nothing to do.
				}
			}
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

		case utils.FUSE_REQ_CONTROL_FILE_CLOSE:
			return nil
		default:
			return utils.ErrFUseProto.As("unknow control code", control)
		}
	}
}
