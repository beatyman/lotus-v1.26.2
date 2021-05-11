package client

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"

	"github.com/gwaylib/errors"
)

type FUseClient struct {
	glock sync.Mutex
	uri   string
	token string
}

func NewFUseClient(uri, token string) *FUseClient {
	return &FUseClient{
		uri:   uri,
		token: token,
	}
}

// SPEC: need system root auth
func (f *FUseClient) Umount(ctx context.Context, mountPoint string) error {
	f.glock.Lock()
	defer f.glock.Unlock()
	// umount
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "umount", "-fl", mountPoint).CombinedOutput()
	if err != nil {
		if strings.Index(string(out), "not mounted") < 0 && strings.Index(string(out), "no mount point") < 0 {
			return errors.As(err)
		}
		// not mounted
	}
	return nil
}

func (f *FUseClient) Mount(ctx context.Context, mountPoint string) error {
	f.glock.Lock()
	defer f.glock.Unlock()
	// checking mountpoint

	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return errors.As(err, mountPoint)
	}

	//root := NewMemNodeFSRoot("test")
	root, err := NewFUseRootFs(f.uri, f.token)
	if err != nil {
		return errors.As(err)
	}
	conn := nodefs.NewFileSystemConnector(root, nil)
	server, err := fuse.NewServer(conn.RawFS(), mountPoint, &fuse.MountOptions{
		Name:  f.uri,
		Debug: os.Getenv("LOTUS_FUSE_DEBUG") == "1",
	})
	if err != nil {
		return errors.As(err)
	}
	end := make(chan os.Signal, 2)
	go func() {
		server.Serve()
		close(end)
	}()

	// when have a exit signal, make umount so the program could shutdown.
	go func() {
		signal.Notify(end, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
		if s := <-end; s != nil {
			log.Infof("auto umount fuse(%s) by kill signal", mountPoint)
			f.Umount(ctx, mountPoint)
		}
	}()
	return nil
}
