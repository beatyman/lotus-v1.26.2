package main

import "github.com/gwaylib/errors"

type DiskPool interface {
	// allocate disk and return repo
	//
	// return
	// repo -- like /data/lotus-cache/1
	Allocate(sid string) (string, error)

	// delete disk data with sector id
	Delete(sid string) error
}

const (
	DISK_MOUNT_ROOT = "/data/lotus-cache/"
)

func NewDiskPool() (DiskPool, error) {
	return nil, errors.New("TODO")
}
