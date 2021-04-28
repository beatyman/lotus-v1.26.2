package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/gwaylib/errors"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
)

type FUseNodeFile struct {
	File   *FUseFile
	node   *FUseNode
	fileLk sync.Mutex
}

func NewReadOnlyFile(node *FUseNode, f *FUseFile) nodefs.File {
	return &FUseNodeFile{
		File: f,
		node: node,
	}
}

// Called upon registering the filehandle in the inode. This
// is useful in that PathFS API, where Create/Open have no
// access to the Inode at hand.
func (f *FUseNodeFile) SetInode(*nodefs.Inode) {
	// nothing need do
}

// The String method is for debug printing.
func (f *FUseNodeFile) String() string {
	return fmt.Sprintf("FUseNodeFile(%s)", f.File.Name())
}

// Wrappers around other File implementations, should return
// the inner file here.
func (f *FUseNodeFile) InnerFile() nodefs.File {
	return f
}

func (f *FUseNodeFile) Read(buf []byte, off int64) (fuse.ReadResult, fuse.Status) {
	var n int
	var err error
	if off < 0 {
		n, err = f.File.Read(buf)
		if err != nil {
			if err != io.EOF {
				return nil, fuse.ToStatus(err)
			}
		}
	} else {
		n, err = f.File.ReadAt(buf, off)
		if err != nil {
			if err != io.EOF {
				return nil, fuse.ToStatus(err)
			}
		}
	}
	if n < 0 {
		n = 0
	}

	// TODO: EOF?
	return fuse.ReadResultData(buf[:n]), fuse.OK
}
func (f *FUseNodeFile) Write(data []byte, off int64) (written uint32, code fuse.Status) {
	return 0, fuse.EPERM
}

// File locking
func (f *FUseNodeFile) GetLk(owner uint64, lk *fuse.FileLock, flags uint32, out *fuse.FileLock) (code fuse.Status) {
	return fuse.ENOSYS
}
func (f *FUseNodeFile) SetLk(owner uint64, lk *fuse.FileLock, flags uint32) (code fuse.Status) {
	return fuse.ENOSYS
}
func (f *FUseNodeFile) SetLkw(owner uint64, lk *fuse.FileLock, flags uint32) (code fuse.Status) {
	return fuse.ENOSYS
}

// Flush is called for close() call on a file descriptor. In
// case of duplicated descriptor, it may be called more than
// once for a file.
func (f *FUseNodeFile) Flush() fuse.Status {
	f.fileLk.Lock()
	defer f.fileLk.Unlock()
	return fuse.ToStatus(f.File.Close())
}

// This is called to before the file handle is forgotten. This
// method has no return value, so nothing can synchronizes on
// the call. Any cleanup that requires specific synchronization or
// could fail with I/O errors should happen in Flush instead.
func (f *FUseNodeFile) Release() {
}
func (f *FUseNodeFile) Fsync(flags int) (code fuse.Status) {
	return fuse.EPERM
}

// The methods below may be called on closed files, due to
// concurrency.  In that case, you should return EBADF.
func (f *FUseNodeFile) Truncate(size uint64) fuse.Status {
	return fuse.EPERM
}
func (f *FUseNodeFile) GetAttr(out *fuse.Attr) fuse.Status {
	stat, err := f.File.Stat()
	if err != nil {
		return fuse.ToStatus(err)
	}
	toFUseFileAttr(stat, out)
	return fuse.OK
}
func (f *FUseNodeFile) Chown(uid uint32, gid uint32) fuse.Status {
	return fuse.EPERM
}
func (f *FUseNodeFile) Chmod(perms uint32) fuse.Status {
	return fuse.EPERM
}
func (f *FUseNodeFile) Utimens(atime *time.Time, mtime *time.Time) fuse.Status {
	return fuse.EPERM
}
func (f *FUseNodeFile) Allocate(off uint64, size uint64, mode uint32) (code fuse.Status) {
	return fuse.EPERM
}

// TODO: redesign read and write.
//
// implement os.File interface
type FUseFile struct {
	ctx       context.Context
	ctxCancel func()

	host       string
	remotePath string
	sid        string
	token      string

	lock       sync.Mutex
	conn       net.Conn
	seekOffset int64

	fileInfo os.FileInfo
}

func OpenFUseFile(host, remotePath, sid, token string) *FUseFile {
	ctx, ctxCancel := context.WithCancel(context.Background())
	return &FUseFile{
		ctx:       ctx,
		ctxCancel: ctxCancel,

		host:       host,
		remotePath: remotePath,
		sid:        sid,
		token:      token,
	}
}

func (f *FUseFile) Name() string {
	return f.remotePath
}

func (f *FUseFile) close() error {
	defer func() {
		f.conn = nil
		f.fileInfo = nil
	}()

	if f.conn != nil {
		return f.conn.Close()
	}
	return nil
}

func (f *FUseFile) open() (net.Conn, error) {
	if f.conn != nil {
		return f.conn, nil
	}
	conn, err := net.Dial("tcp", f.host)
	if err != nil {
		return nil, errors.As(err)
	}

	params := []byte(fmt.Sprintf(`{"Method":"Open","Path":"%s","Sid":"%s","Auth":"%s"}`, f.remotePath, f.sid, f.token))
	if err := utils.WriteFUseTextReq(conn, params); err != nil {
		conn.Close()
		return nil, errors.As(err, f.remotePath)
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		conn.Close()
		return nil, errors.As(err, f.remotePath)
	}
	switch resp["Code"] {
	case "200":
		// pass
	case "404":
		return nil, &os.PathError{"readRemote", f.remotePath, _errNotExist}
	default:
		conn.Close()
		return nil, errors.Parse(resp["Err"].(string))
	}
	f.conn = conn
	return f.conn, nil
}

func (f *FUseFile) readRemote(b []byte, off int64) (int, error) {
	conn, err := f.open()
	if err != nil {
		return 0, err
	}

	// request read data
	if err := utils.WriteFUseReqHeader(conn, utils.FUSE_REQ_CONTROL_FILE_READ, len(b)); err != nil {
		f.close()
		return 0, errors.As(err, f.remotePath, off, len(b))
	}
	offB := make([]byte, 8)
	binary.PutVarint(offB, off)
	if _, err := f.conn.Write(offB); err != nil {
		f.close()
		return 0, errors.As(err, f.remotePath, off, len(b))
	}

	// read
	control, err := utils.ReadFUseRespHeader(conn)
	if err != nil {
		f.close()
		return 0, errors.As(err, f.remotePath, off, len(b))
	}
	// has text resp, it should be some errors.
	if control == utils.FUSE_RESP_CONTROL_TEXT {
		resp, err := utils.ReadFUseRespText(conn)
		if err != nil {
			f.close()
			return 0, errors.As(err, f.remotePath, off, len(b))
		}
		return 0, errors.Parse(resp["Err"].(string)).As(f.remotePath, off, len(b))
	}

	read := 0
	n := 0
	bufLen := len(b)
	for {
		n, err = conn.Read(b[read:])
		read += n
		if err != nil {
			f.close()
			break
		}
		if n > 0 && read < bufLen {
			continue
		}
		break
	}
	f.seekOffset = off + int64(read)
	return read, errors.As(err, f.remotePath, off, len(b))
}

func (f *FUseFile) writeRemote(b []byte, off int64) (int64, error) {
	conn, err := f.open()
	if err != nil {
		return 0, err
	}

	// prepare write
	if err := utils.WriteFUseReqHeader(conn, utils.FUSE_REQ_CONTROL_FILE_WRITE, len(b)); err != nil {
		f.close()
		return 0, errors.As(err)
	}
	offB := make([]byte, 8)
	binary.PutVarint(offB, off)
	if _, err := conn.Write(offB); err != nil {
		f.close()
		return 0, errors.As(err)
	}

	// write data
	n, err := conn.Write(b)
	f.seekOffset = off + int64(n)
	if err != nil {
		f.close()
		return int64(n), errors.As(err)
	}

	return int64(n), nil
}

func (f *FUseFile) Close() error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.ctxCancel != nil {
		f.ctxCancel()
	}
	if f.conn != nil {
		utils.WriteFUseReqHeader(f.conn, utils.FUSE_REQ_CONTROL_FILE_CLOSE, 0)
	}
	return f.close()
}

func (f *FUseFile) Seek(offset int64, whence int) (ret int64, err error) {
	if whence != 0 {
		return 0, errors.New("unsupport whence not zero")
	}

	f.lock.Lock()
	defer f.lock.Unlock()
	f.seekOffset = offset
	return f.seekOffset, nil
}

func (f *FUseFile) read(b []byte) (n int, err error) {
	written, err := f.readRemote(b, f.seekOffset)
	if err != nil {
		if !utils.ErrEOF.Equal(err) {
			return int(written), err
		}
	}
	if written < len(b) {
		return int(written), io.EOF
	}
	return int(written), err
}
func (f *FUseFile) Read(b []byte) (n int, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	n, err = f.read(b)
	if err != nil {
		if io.EOF != err {
			return n, err
		}
		// ignore io.EOF for Read
		// TODO: confirm this is special?
	}
	return n, nil

}
func (f *FUseFile) ReadAt(b []byte, off int64) (n int, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if off < 0 {
		return 0, &os.PathError{"readat", f.remotePath, errors.New("negative offset")}
	}
	f.seekOffset = off

	n, err = f.read(b)
	if n < len(b) {
		return n, err
	}
	return n, nil
}

func (f *FUseFile) Write(b []byte) (n int, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.fileInfo = nil

	written, err := f.writeRemote(b, f.seekOffset)
	return int(written), err
}

func (f *FUseFile) Stat() (os.FileInfo, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.fileInfo != nil {
		return f.fileInfo, nil
	}

	conn, err := f.open()
	if err != nil {
		return nil, err
	}

	// request read data
	if err := utils.WriteFUseReqHeader(conn, utils.FUSE_REQ_CONTROL_FILE_STAT, 0); err != nil {
		f.close()
		return nil, errors.As(err, f.remotePath)
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		f.close()
		return nil, errors.As(err, f.remotePath)
	}
	if resp["Code"] != "200" {
		f.close()
		return nil, errors.Parse(resp["Err"].(string))
	}
	stat, ok := resp["Data"].(map[string]interface{})
	if !ok {
		f.close()
		return nil, errors.New("error protocol").As(resp)
	}
	mTime, err := time.Parse(time.RFC3339Nano, stat["FileModTime"].(string))
	if err != nil {
		f.close()
		return nil, errors.As(err)
	}
	f.fileInfo = &utils.ServerFileStat{
		FileName:    fmt.Sprint(stat["FileName"]),
		IsDirFile:   stat["IsDirFile"].(bool),
		FileSize:    int64(stat["FileSize"].(float64)),
		FileModTime: mTime,
	}
	return f.fileInfo, nil
}
func (f *FUseFile) Truncate(size int64) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.fileInfo = nil

	conn, err := f.open()
	if err != nil {
		return err
	}

	// request read data
	if err := utils.WriteFUseReqHeader(conn, utils.FUSE_REQ_CONTROL_FILE_TRUNC, 0); err != nil {
		f.close()
		return errors.As(err)
	}
	sizeB := make([]byte, 8)
	binary.PutVarint(sizeB, size)
	if _, err := conn.Write(sizeB); err != nil {
		f.close()
		return errors.As(err)
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		f.close()
		return errors.As(err)
	}
	if resp["Code"] != "200" {
		f.close()
		return errors.Parse(resp["Err"].(string))
	}
	return nil
}
