// Copyright 2016 the Go-FUSE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
)

// NewMemNodeFSRoot creates an in-memory node-based filesystem. Files
// are written into a backing store under the given prefix.
func NewMemNodeFSRoot(prefix string) nodefs.Node {
	fs := &memNodeFs{
		backingStorePrefix: prefix,
	}
	fs.root = fs.newNode()
	return fs.root
}

type memNodeFs struct {
	backingStorePrefix string
	root               *memNode

	mutex    sync.Mutex
	nextFree int
}

func (fs *memNodeFs) String() string {
	fmt.Println("memNodeFs.String")
	return fmt.Sprintf("MemNodeFs(%s)", fs.backingStorePrefix)
}

func (fs *memNodeFs) Root() nodefs.Node {
	fmt.Println("memNodeFs.Root")
	return fs.root
}

func (fs *memNodeFs) SetDebug(bool) {
	fmt.Println("memNodeFs.SetDebug")
}

func (fs *memNodeFs) OnMount(*nodefs.FileSystemConnector) {
	fmt.Println("memNodeFs.OnMount")
}

func (fs *memNodeFs) OnUnmount() {
	fmt.Println("memNodeFs.OnUnmount")
}

func (fs *memNodeFs) newNode() *memNode {
	fmt.Println("memNodeFs.newNode")
	fs.mutex.Lock()
	id := fs.nextFree
	fs.nextFree++
	fs.mutex.Unlock()
	n := &memNode{
		Node: nodefs.NewDefaultNode(),
		fs:   fs,
		id:   id,
	}
	now := time.Now()
	n.info.SetTimes(&now, &now, &now)
	n.info.Mode = fuse.S_IFDIR | 0777
	return n
}

func (fs *memNodeFs) Filename(n *nodefs.Inode) string {
	fmt.Println("memNodeFs.Filename")
	mn := n.Node().(*memNode)
	return mn.filename()
}

type memNode struct {
	nodefs.Node
	fs *memNodeFs
	id int

	mu   sync.Mutex
	link string
	info fuse.Attr
}

func (n *memNode) filename() string {
	return fmt.Sprintf("%s%d", n.fs.backingStorePrefix, n.id)
}

func (n *memNode) Deletable() bool {
	fmt.Println("memNode.Deleteable")
	return false
}

func (n *memNode) Readlink(c *fuse.Context) ([]byte, fuse.Status) {
	fmt.Println("memNode.Readlink")
	n.mu.Lock()
	defer n.mu.Unlock()
	return []byte(n.link), fuse.OK
}

func (n *memNode) StatFs() *fuse.StatfsOut {
	fmt.Println("memNode.StatFs")
	root := "/data/zfs"
	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(root, &fs); err != nil {
		log.Error(errors.As(err))
		return &fuse.StatfsOut{}
	}

	return &fuse.StatfsOut{
		Blocks:  fs.Blocks,
		Bfree:   fs.Bfree,
		Bavail:  fs.Bavail,
		Files:   fs.Files,
		Ffree:   fs.Ffree,
		Bsize:   uint32(fs.Bsize),
		NameLen: uint32(fs.Namelen),
		Frsize:  uint32(fs.Frsize),
	}
}

func (n *memNode) Mkdir(name string, mode uint32, context *fuse.Context) (newNode *nodefs.Inode, code fuse.Status) {
	fmt.Println("memNode.Mkdir")
	ch := n.fs.newNode()
	ch.info.Mode = mode | fuse.S_IFDIR
	n.Inode().NewChild(name, true, ch)
	return ch.Inode(), fuse.OK
}

func (n *memNode) Unlink(name string, context *fuse.Context) (code fuse.Status) {
	fmt.Println("memNode.Unlink")
	ch := n.Inode().RmChild(name)
	if ch == nil {
		return fuse.ENOENT
	}
	return fuse.OK
}

func (n *memNode) Rmdir(name string, context *fuse.Context) (code fuse.Status) {
	fmt.Println("memNode.Rmdir")
	return n.Unlink(name, context)
}

func (n *memNode) Symlink(name string, content string, context *fuse.Context) (newNode *nodefs.Inode, code fuse.Status) {
	fmt.Println("memNode.Symlink")
	ch := n.fs.newNode()
	ch.info.Mode = fuse.S_IFLNK | 0777
	ch.link = content
	n.Inode().NewChild(name, false, ch)
	return ch.Inode(), fuse.OK
}

func (n *memNode) Rename(oldName string, newParent nodefs.Node, newName string, context *fuse.Context) (code fuse.Status) {
	fmt.Println("memNode.Rename")
	ch := n.Inode().RmChild(oldName)
	newParent.Inode().RmChild(newName)
	newParent.Inode().AddChild(newName, ch)
	return fuse.OK
}

func (n *memNode) Link(name string, existing nodefs.Node, context *fuse.Context) (*nodefs.Inode, fuse.Status) {
	fmt.Println("memNode.Link")
	n.Inode().AddChild(name, existing.Inode())
	return existing.Inode(), fuse.OK
}

func (n *memNode) Create(name string, flags uint32, mode uint32, context *fuse.Context) (file nodefs.File, node *nodefs.Inode, code fuse.Status) {
	fmt.Println("memNode.Create")
	ch := n.fs.newNode()
	ch.info.Mode = mode | fuse.S_IFREG

	f, err := os.Create(ch.filename())
	if err != nil {
		return nil, nil, fuse.ToStatus(err)
	}
	n.Inode().NewChild(name, false, ch)
	return ch.newFile(f), ch.Inode(), fuse.OK
}

type memNodeFile struct {
	nodefs.File
	node *memNode
}

func (n *memNodeFile) String() string {
	return fmt.Sprintf("memNodeFile(%s)", n.File.String())
}

func (n *memNodeFile) InnerFile() nodefs.File {
	return n.File
}

func (n *memNodeFile) Flush() fuse.Status {
	code := n.File.Flush()

	if !code.Ok() {
		return code
	}

	st := syscall.Stat_t{}
	err := syscall.Stat(n.node.filename(), &st)

	n.node.mu.Lock()
	defer n.node.mu.Unlock()
	n.node.info.Size = uint64(st.Size)
	n.node.info.Blocks = uint64(st.Blocks)
	return fuse.ToStatus(err)
}

func (n *memNode) newFile(f *os.File) nodefs.File {
	return &memNodeFile{
		File: nodefs.NewLoopbackFile(f),
		node: n,
	}
}

func (n *memNode) Open(flags uint32, context *fuse.Context) (file nodefs.File, code fuse.Status) {
	f, err := os.OpenFile(n.filename(), int(flags), 0666)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}

	return n.newFile(f), fuse.OK
}

func (n *memNode) GetAttr(fi *fuse.Attr, file nodefs.File, context *fuse.Context) (code fuse.Status) {
	n.mu.Lock()
	defer n.mu.Unlock()

	*fi = n.info
	return fuse.OK
}

func (n *memNode) Truncate(file nodefs.File, size uint64, context *fuse.Context) (code fuse.Status) {
	if file != nil {
		code = file.Truncate(size)
	} else {
		err := os.Truncate(n.filename(), int64(size))
		code = fuse.ToStatus(err)
	}
	if code.Ok() {
		now := time.Now()

		n.mu.Lock()
		defer n.mu.Unlock()

		n.info.SetTimes(nil, nil, &now)
		// TODO - should update mtime too?
		n.info.Size = size
	}
	return code
}

func (n *memNode) Utimens(file nodefs.File, atime *time.Time, mtime *time.Time, context *fuse.Context) (code fuse.Status) {
	c := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()

	n.info.SetTimes(atime, mtime, &c)
	return fuse.OK
}

func (n *memNode) Chmod(file nodefs.File, perms uint32, context *fuse.Context) (code fuse.Status) {
	n.info.Mode = (n.info.Mode &^ 07777) | perms
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	n.info.SetTimes(nil, nil, &now)
	return fuse.OK
}

func (n *memNode) Chown(file nodefs.File, uid uint32, gid uint32, context *fuse.Context) (code fuse.Status) {
	n.info.Uid = uid
	n.info.Gid = gid
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	n.info.SetTimes(nil, nil, &now)
	return fuse.OK
}
