package fsutil

import "os"

type PartialFile interface {
	Close() error

	Write(b []byte) (n int, err error)
	Read(b []byte) (n int, err error)
	ReadAt(b []byte, off int64) (n int, err error)
	Seek(offset int64, whence int) (ret int64, err error)

	Truncate(size int64) error
	Stat() (os.FileInfo, error)
}
