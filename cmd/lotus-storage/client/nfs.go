package client

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gwaylib/errors"
)

type NFSClient struct {
	Uri   string
	Token string
}

func NewNFSClient(uri, token string) *NFSClient {
	return &NFSClient{Uri: uri, Token: token}
}

func (n *NFSClient) Umount(ctx context.Context, mountPoint string) error {
	// umount
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "umount", "-fl", mountPoint).CombinedOutput()
	if err == nil {
		return nil
	}

	if strings.Index(string(out), "not mounted") > -1 {
		// not mounted
		return nil
	}
	if strings.Index(string(out), "no mount point") > -1 {
		// no mount point
		return nil
	}
	return errors.As(err, mountPoint)
}

func (n *NFSClient) Mount(ctx context.Context, mountPoint string) error {
	// remove link
	info, err := os.Lstat(mountPoint)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err, mountPoint)
		}
		// name not exist.
	} else {
		// clean the mount point
		m := info.Mode()
		if m&os.ModeSymlink == os.ModeSymlink {
			// clean old link
			if err := os.Remove(mountPoint); err != nil {
				return errors.As(err, mountPoint)
			}
		} else if !m.IsDir() {
			return errors.New("file has existed").As(mountPoint)
		}
	}
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return errors.As(err)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	if out, err := exec.CommandContext(timeoutCtx,
		"mount",
		"-t", "nfs",
		"-o", "vers=3,rw,nolock,intr,proto=tcp,rsize=1048576,wsize=1048576,hard,timeo=7,retrans=10,actimeo=10,retry=5",
		n.Uri,
		mountPoint,
	).CombinedOutput(); err != nil {
		cancel()
		return errors.As(err, string(out))
	}
	cancel()
	return nil
}
