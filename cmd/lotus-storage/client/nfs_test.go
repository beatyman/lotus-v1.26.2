package client

import (
	"context"
	"crypto/md5"
	"fmt"
	"syscall"
	"testing"
)

func TestNFSMd5(t *testing.T) {
	in := []byte("d41d8cd98f00b204e9800998ecf8427e")
	nfsAuth := fmt.Sprintf("%x", md5.Sum(in))
	if string(nfsAuth) != "74be16979710d4c4e7c6647856088456" {
		t.Fatal("md5 not match")
	}
}

func TestNFSClient(t *testing.T) {
	// need root auth
	return

	ctx := context.TODO()
	mountPoint := "./tmp"
	nc := NewNFSClient("127.0.0.1:1332", "74be16979710d4c4e7c6647856088456")
	if err := nc.Mount(ctx, mountPoint); err != nil {
		t.Fatal(err)
	}

	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(mountPoint, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Blocks == 0 {
		t.Fatalf("%+v", fs)
	}
	if err := nc.Umount(ctx, mountPoint); err != nil {
		t.Fatal(err)
	}
}
