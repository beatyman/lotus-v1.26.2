package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
	lru "github.com/hashicorp/golang-lru"
	"github.com/willscott/go-nfs"
)

const (
	_NFS_LIMIT       = 102400
	FILE_PATH_MAXNUM = 32 // [0:16] for auth, [16:32] for path md5
)

func hasPrefix(path, prefix []string) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i, e := range prefix {
		if path[i] != e {
			return false
		}
	}
	return true
}

type ROAuthFS struct {
	auth string
	billy.Filesystem
}

// Capabilities exports the filesystem as readonly
func (ROAuthFS) Capabilities() billy.Capability {
	return billy.ReadCapability | billy.SeekCapability
	//return billy.AllCapabilities
}

func (ROAuthFS) Root() string {
	return _repoFlag
}

type AuthFS struct {
	auth string
	billy.Filesystem
}

func (AuthFS) Root() string {
	return _repoFlag
}

// Chmod changes mode
func (fs AuthFS) Chmod(name string, mode os.FileMode) error {
	err := os.Chmod(fs.Join(fs.Root(), name), mode)
	if err != nil {
		log.Warn(errors.As(err))
	}
	return err
}

// Lchown changes ownership
func (fs AuthFS) Lchown(name string, uid, gid int) error {
	err := os.Lchown(fs.Join(fs.Root(), name), uid, gid)
	if err != nil {
		log.Warn(errors.As(err))
	}
	return err
}

// Chown changes ownership
func (fs AuthFS) Chown(name string, uid, gid int) error {
	err := os.Chown(fs.Join(fs.Root(), name), uid, gid)
	if err != nil {
		log.Warn(errors.As(err))
	}
	return err
}

// Chtimes changes access time
func (fs AuthFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	err := os.Chtimes(fs.Join(fs.Root(), name), atime, mtime)
	if err != nil {
		return errors.As(err)
	}
	return err
}

// NFSAuthHandler returns a NFS backing that exposes a given file system in response to all mount requests.
type NFSAuthHandler struct {
	rootfs  billy.Filesystem
	visitLk sync.Mutex

	activeHandles *lru.Cache
}

type entry struct {
	f billy.Filesystem
	p []string
}

func NewNFSAuthHandler() nfs.Handler {
	cache, _ := lru.New(_NFS_LIMIT)
	return &NFSAuthHandler{
		rootfs: osfs.New(_repoFlag),

		activeHandles: cache,
	}
}

// Mount backs Mount RPC Requests, allowing for access control policies.
func (h *NFSAuthHandler) Mount(ctx context.Context, conn net.Conn, req nfs.MountRequest) (status nfs.MountStatus, hndl billy.Filesystem, auths []nfs.AuthFlavor) {
	// TODO: against auth attack.

	remoteAddr := conn.RemoteAddr()

	write := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"write")))
	read := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"read")))
	switch filepath.Base(string(req.Dirpath)) {
	case write:
		hndl = &AuthFS{auth: write, Filesystem: h.rootfs}
	case read:
		hndl = &ROAuthFS{auth: read, Filesystem: h.rootfs}
	default:
		log.Infof("unkown path, from: %s,path: %s", remoteAddr, string(req.Dirpath))
		status = nfs.MountStatusErrPerm
		return
	}

	status = nfs.MountStatusOk
	auths = []nfs.AuthFlavor{nfs.AuthFlavorNull}
	return
}

// Change provides an interface for updating file attributes.
func (h *NFSAuthHandler) Change(fs billy.Filesystem) billy.Change {
	// TODO: auth
	if c, ok := h.rootfs.(billy.Change); ok {
		return c
	}
	return nil
}

// FSStat provides information about a filesystem.
func (h *NFSAuthHandler) FSStat(ctx context.Context, f billy.Filesystem, s *nfs.FSStat) error {
	// implement the df -h
	root := h.rootfs.Root()
	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(root, &fs); err != nil {
		return errors.As(err, root)
	}

	s.TotalSize = fs.Blocks * uint64(fs.Bsize)
	s.FreeSize = fs.Bfree * uint64(fs.Bsize)
	s.AvailableSize = fs.Bavail * uint64(fs.Bsize)
	s.TotalFiles = fs.Files
	s.FreeFiles = fs.Ffree
	return nil
}

// ToHandle handled by CachingHandler
func (h *NFSAuthHandler) ToHandle(f billy.Filesystem, path []string) []byte {
	// TODO: concurrency testing
	id := uuid.New()
	h.activeHandles.Add(id, entry{f, path})
	b, _ := id.MarshalBinary()
	return b

	dst := []byte{}
	auth := ""
	authfs, ok := f.(*AuthFS)
	if ok {
		auth = authfs.auth
	} else {
		rofs, ok := f.(*ROAuthFS)
		if !ok {
			log.Print("DEBUG: unknow auth", f)
			return dst
		}
		auth = rofs.auth
	}
	if len(auth) == 0 {
		log.Error("auth len error: %d != %d", len(auth), md5.Size)
		return dst
	}

	// append path key
	sPath := filepath.Join(path...)

	dst = append(dst, []byte(auth)...)
	dst = append(dst, []byte(sPath)...)
	sessionKey := md5.Sum(dst)

	// save key to db
	h.visitLk.Lock()
	defer h.visitLk.Unlock()
	if _, _, err := GetSessionFile(fmt.Sprintf("%x", sessionKey[:])); err != nil {
		if !errors.ErrNoData.Equal(err) {
			log.Error(errors.As(err))
			return sessionKey[:]
		}

		// data not found
		if err := AddSessionFile(fmt.Sprintf("%x", sessionKey[:]), auth, sPath); err != nil {
			log.Error(errors.As(err))
			return sessionKey[:]
		}
	}

	return sessionKey[:]
}

// FromHandle handled by CachingHandler
func (h *NFSAuthHandler) FromHandle(fh []byte) (billy.Filesystem, []string, error) {
	// TODO: concurrency testing
	id, err := uuid.FromBytes(fh)
	if err != nil {
		return nil, []string{}, err
	}

	if cache, ok := h.activeHandles.Get(id); ok {
		f, ok := cache.(entry)
		for _, k := range h.activeHandles.Keys() {
			e, _ := h.activeHandles.Peek(k)
			candidate := e.(entry)
			if hasPrefix(f.p, candidate.p) {
				_, _ = h.activeHandles.Get(k)
			}
		}
		if ok {
			return f.f, f.p, nil
		}
	}
	return nil, []string{}, &nfs.NFSStatusError{NFSStatus: nfs.NFSStatusStale}

	sessionKey := fmt.Sprintf("%x", fh)
	auth, paths, err := GetSessionFile(sessionKey)
	if err != nil {
		log.Warnf("GetPath failed:%+v,err:%s", sessionKey, err.Error())
		return nil, []string{}, &nfs.NFSStatusError{NFSStatus: nfs.NFSStatusStale}
	}
	path := strings.Split(paths, string(filepath.Separator))

	write := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"write")))
	read := fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"read")))
	switch auth {
	case write:
		return &AuthFS{auth: auth, Filesystem: h.rootfs}, path, nil
	case read:
		return &ROAuthFS{auth: auth, Filesystem: h.rootfs}, path, nil
	}

	log.Infof("unknow auth:%s", auth)
	return nil, []string{}, &nfs.NFSStatusError{NFSStatus: nfs.NFSStatusStale}
}

// HandleLImit handled by cachingHandler
func (h *NFSAuthHandler) HandleLimit() int {
	// ulimit
	// TODO: same as the os open_limit
	return _NFS_LIMIT
}
