package client

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
)

const (
	_rootPath = "/data/zfs"
)

func NewFUseRootFs(uri, token string) (nodefs.Node, error) {
	fs := &FUseNodeFs{
		host: uri,
		auth: token,
	}
	fs.root = fs.newNode("")
	return fs.root, nil
}

type FUseNode struct {
	inode        *nodefs.Inode
	fs           *FUseNodeFs
	relativePath string

	mu   sync.Mutex
	attr *fuse.Attr
}

type FUseNodeFs struct {
	host string
	auth string

	connLk sync.Mutex
	conn   net.Conn

	root *FUseNode
}

func (fs *FUseNodeFs) close() error {
	defer func() {
		fs.conn = nil
	}()
	if fs.conn != nil {
		return fs.conn.Close()
	}
	return nil
}
func (fs *FUseNodeFs) open() (net.Conn, error) {
	if fs.conn != nil {
		return fs.conn, nil
	}
	conn, err := net.Dial("tcp", fs.host)
	if err != nil {
		return nil, errors.As(err)
	}
	fs.conn = conn
	return fs.conn, nil
}
func (fs FUseNodeFs) FileList(relativePath string) ([]os.FileInfo, error) {
	fs.connLk.Lock()
	defer fs.connLk.Unlock()

	conn, err := fs.open()
	if err != nil {
		return nil, errors.As(err)
	}
	params := []byte(fmt.Sprintf(`{"Method":"List","Path":"%s"}`, relativePath))
	if err := utils.WriteFUseTextReq(conn, params); err != nil {
		fs.close()
		return nil, errors.As(err)
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		fs.close()
		return nil, errors.As(err)
	}
	switch resp["Code"] {
	case "200":
		// pass
	case "404":
		return nil, &os.PathError{"FUseNodeFs.Stat", relativePath, _errNotExist}
	default:
		fs.close()
		return nil, errors.Parse(resp["Err"].(string))
	}
	files, ok := resp["Data"].([]interface{})
	if !ok {
		fs.close()
		return nil, errors.New("error protocol").As(resp)
	}
	result := []os.FileInfo{}
	for _, file := range files {
		stat, ok := file.(map[string]interface{})
		if !ok {
			fs.close()
			return nil, errors.New("error protocol").As(resp)
		}
		mTime, err := time.Parse(time.RFC3339Nano, stat["FileModTime"].(string))
		if err != nil {
			fs.close()
			return nil, errors.As(err)
		}
		result = append(result, &utils.ServerFileStat{
			FileName:    fmt.Sprint(stat["FileName"]),
			IsDirFile:   stat["IsDirFile"].(bool),
			FileSize:    int64(stat["FileSize"].(float64)),
			FileModTime: mTime,
		})
	}
	return result, nil
}
func (fs FUseNodeFs) FileStat(relativePath string) (os.FileInfo, error) {
	fs.connLk.Lock()
	defer fs.connLk.Unlock()

	conn, err := fs.open()
	if err != nil {
		return nil, errors.As(err)
	}
	params := []byte(fmt.Sprintf(`{"Method":"Stat","Path":"%s"}`, relativePath))
	if err := utils.WriteFUseTextReq(conn, params); err != nil {
		fs.close()
		return nil, errors.As(err)
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		fs.close()
		return nil, errors.As(err)
	}
	switch resp["Code"] {
	case "200":
		// pass
	case "404":
		return nil, &os.PathError{"FUseNodeFs.Stat", relativePath, _errNotExist}
	default:
		fs.close()
		return nil, errors.Parse(resp["Err"].(string))
	}
	stat, ok := resp["Data"].(map[string]interface{})
	if !ok {
		fs.close()
		return nil, errors.New("error protocol").As(resp)
	}
	mTime, err := time.Parse(time.RFC3339Nano, stat["FileModTime"].(string))
	if err != nil {
		fs.close()
		return nil, errors.As(err)
	}
	return &utils.ServerFileStat{
		FileName:    fmt.Sprint(stat["FileName"]),
		IsDirFile:   stat["IsDirFile"].(bool),
		FileSize:    int64(stat["FileSize"].(float64)),
		FileModTime: mTime,
	}, nil
}
func (fs FUseNodeFs) StatFs() *fuse.StatfsOut {
	fs.connLk.Lock()
	defer fs.connLk.Unlock()
	conn, err := fs.open()
	if err != nil {
		log.Error(errors.As(err))
		return &fuse.StatfsOut{}
	}
	params := []byte(fmt.Sprintf(`{"Method":"Cap"}`))
	if err := utils.WriteFUseTextReq(conn, params); err != nil {
		log.Error(errors.As(err))
		return &fuse.StatfsOut{}
	}
	resp, err := utils.ReadFUseTextResp(conn)
	if err != nil {
		log.Error(errors.As(err))
		return &fuse.StatfsOut{}
	}
	if resp["Code"] != "200" {
		log.Error(errors.Parse(resp["Err"].(string)))
		return &fuse.StatfsOut{}
	}
	st := resp["Data"].(map[string]interface{})
	return &fuse.StatfsOut{
		Blocks:  uint64(st["Blocks"].(float64)),
		Bfree:   uint64(st["Bfree"].(float64)),
		Bavail:  uint64(st["Bavail"].(float64)),
		Files:   uint64(st["Files"].(float64)),
		Ffree:   uint64(st["Ffree"].(float64)),
		Bsize:   uint32(st["Bsize"].(float64)),
		NameLen: uint32(st["Namelen"].(float64)),
		Frsize:  uint32(st["Frsize"].(float64)),
	}
}

func (fs *FUseNodeFs) String() string {
	return fmt.Sprintf("FUseNodeFs(%s)", fs.host)
}

func (fs *FUseNodeFs) Root() nodefs.Node {
	return fs.root
}

func (fs *FUseNodeFs) SetDebug(bool) {
}

func (fs *FUseNodeFs) OnMount(*nodefs.FileSystemConnector) {
}

func (fs *FUseNodeFs) OnUnmount() {
}

func (fs *FUseNodeFs) newNode(relativePath string) *FUseNode {
	n := &FUseNode{
		fs:           fs,
		relativePath: relativePath,
	}

	return n
}

func (fs *FUseNodeFs) Filename(n *nodefs.Inode) string {
	mn := n.Node().(*FUseNode)
	return mn.filename()
}

func (fn *FUseNode) filename() string {
	return fn.relativePath
}

func (fn *FUseNode) Deletable() bool {
	return false
}

func (fn *FUseNode) Readlink(c *fuse.Context) ([]byte, fuse.Status) {
	return []byte{}, fuse.ENOSYS
}

func (fn *FUseNode) StatFs() *fuse.StatfsOut {
	return fn.fs.StatFs()
}

func (fn *FUseNode) Inode() *nodefs.Inode {
	return fn.inode
}
func (fn *FUseNode) SetInode(node *nodefs.Inode) {
	fn.inode = node
}
func (fn *FUseNode) OnUnmount() {
}

func (fn *FUseNode) OnMount(conn *nodefs.FileSystemConnector) {
}
func (fn *FUseNode) OnForget() {
}
func (fn *FUseNode) Mknod(name string, mode uint32, dev uint32, context *fuse.Context) (newNode *nodefs.Inode, code fuse.Status) {
	return nil, fuse.ENOSYS
}
func (fn *FUseNode) Mkdir(name string, mode uint32, context *fuse.Context) (newNode *nodefs.Inode, code fuse.Status) {
	return nil, fuse.ENOSYS
}

func (fn *FUseNode) Unlink(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

func (fn *FUseNode) Rmdir(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

func (fn *FUseNode) Symlink(name string, content string, context *fuse.Context) (newNode *nodefs.Inode, code fuse.Status) {
	return nil, fuse.ENOSYS
}

func (fn *FUseNode) Rename(oldName string, newParent nodefs.Node, newName string, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

func (fn *FUseNode) Link(name string, existing nodefs.Node, context *fuse.Context) (*nodefs.Inode, fuse.Status) {
	return nil, fuse.ENOSYS
}

func (fn *FUseNode) Truncate(file nodefs.File, size uint64, context *fuse.Context) (code fuse.Status) {
	return fuse.EPERM
}

func (fn *FUseNode) Utimens(file nodefs.File, atime *time.Time, mtime *time.Time, context *fuse.Context) (code fuse.Status) {
	return fuse.EPERM
}

func (fn *FUseNode) Chmod(file nodefs.File, perms uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.EPERM
}

func (fn *FUseNode) Chown(file nodefs.File, uid uint32, gid uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.EPERM
}

func (fn *FUseNode) Access(mode uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.OK
}
func (fn *FUseNode) Fallocate(file nodefs.File, off uint64, size uint64, mode uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

// GetLk returns existing lock information for file.
func (fn *FUseNode) GetLk(file nodefs.File, owner uint64, lk *fuse.FileLock, flags uint32, out *fuse.FileLock, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

// Sets or clears the lock described by lk on file.
func (fn *FUseNode) SetLk(file nodefs.File, owner uint64, lk *fuse.FileLock, flags uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

// Sets or clears the lock described by lk. This call blocks until the operation can be completed.
func (fn *FUseNode) SetLkw(file nodefs.File, owner uint64, lk *fuse.FileLock, flags uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ENOSYS
}

func (fn *FUseNode) Create(name string, flags uint32, mode uint32, context *fuse.Context) (file nodefs.File, node *nodefs.Inode, code fuse.Status) {
	return nil, nil, fuse.ENOSYS
}

func toFUseFileAttr(stat os.FileInfo, fi *fuse.Attr) {
	modTime := stat.ModTime()
	fi.Size = uint64(stat.Size())
	fi.SetTimes(&modTime, &modTime, &modTime)
	if stat.IsDir() {
		fi.Mode = fuse.S_IFDIR | 0755
	} else {
		fi.Mode = fuse.S_IFREG | 0644
	}
	return
}

// XAttrs
func (fn *FUseNode) GetXAttr(attribute string, context *fuse.Context) (data []byte, code fuse.Status) {
	return nil, fuse.ENOSYS
}
func (fn *FUseNode) RemoveXAttr(attr string, context *fuse.Context) fuse.Status {
	return fuse.ENOSYS
}
func (fn *FUseNode) SetXAttr(attr string, data []byte, flags int, context *fuse.Context) fuse.Status {

	return fuse.ENOSYS
}
func (fn *FUseNode) ListXAttr(context *fuse.Context) (attrs []string, code fuse.Status) {
	return nil, fuse.ENOSYS
}

func (fn *FUseNode) GetAttr(fi *fuse.Attr, file nodefs.File, context *fuse.Context) (code fuse.Status) {
	if file != nil {
		return file.GetAttr(fi)
	}
	stat, err := fn.fs.FileStat(fn.relativePath)
	if err != nil {
		return fuse.ToStatus(err)
	}
	toFUseFileAttr(stat, fi)
	return fuse.OK
}

func (fn *FUseNode) newFile(f *File) nodefs.File {
	return &FUseNodeFile{
		File: f,
		node: fn,
	}
}

func (fn *FUseNode) Lookup(out *fuse.Attr, name string, context *fuse.Context) (node *nodefs.Inode, code fuse.Status) {

	path := filepath.Join(fn.relativePath, name)
	nStat, err := fn.fs.FileStat(path)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}

	fn.mu.Lock()
	inode := fn.Inode()
	key := filepath.Join(fn.relativePath, name)
	ch := inode.GetChild(key)
	if ch == nil {
		child := fn.fs.newNode(key)
		ch = inode.NewChild(key, nStat.IsDir(), child)
	}
	fn.mu.Unlock()

	toFUseFileAttr(nStat, out)
	return ch, fuse.OK

}

func (fn *FUseNode) Open(flags uint32, context *fuse.Context) (file nodefs.File, code fuse.Status) {
	f := OpenFile(fn.fs.host, fn.relativePath, "", fn.fs.auth)
	return fn.newFile(f), fuse.OK
}
func (fn *FUseNode) OpenDir(context *fuse.Context) ([]fuse.DirEntry, fuse.Status) {
	nStat, err := fn.fs.FileStat(fn.relativePath)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	if !nStat.IsDir() {
		// can't open not the directory
		return nil, fuse.EPERM
	}

	files, err := fn.fs.FileList(fn.relativePath)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	result := []fuse.DirEntry{}
	for _, file := range files {
		fileName := file.Name()
		if fileName == "." {
			continue
		}
		// reset children
		fn.mu.Lock()
		inode := fn.Inode()
		key := filepath.Join(fn.relativePath, fileName)
		ch := inode.GetChild(key)
		if ch == nil {
			child := fn.fs.newNode(key)
			ch = inode.NewChild(key, file.IsDir(), child)
		}
		fn.mu.Unlock()

		fi := &fuse.Attr{}
		toFUseFileAttr(file, fi)
		entry := fuse.DirEntry{
			Mode: fi.Mode,
			Name: fileName,
		}
		result = append(result, entry)
	}

	return result, fuse.OK
}

func (n *FUseNode) Read(file nodefs.File, dest []byte, off int64, context *fuse.Context) (fuse.ReadResult, fuse.Status) {
	if file != nil {
		return file.Read(dest, off)
	}
	return nil, fuse.ENOSYS
}
func (n *FUseNode) Write(file nodefs.File, data []byte, off int64, context *fuse.Context) (written uint32, code fuse.Status) {
	if file != nil {
		return file.Write(data, off)
	}
	return 0, fuse.ENOSYS
}
