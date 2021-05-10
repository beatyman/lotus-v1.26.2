package fsutil

import (
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("fsutil")

const FallocFlPunchHole = 0x02 // linux/falloc.h

func Deallocate(file PartialFile, offset int64, length int64) error {
	// close this function by hlm
	return nil

	//	if length == 0 {
	//		return nil
	//	}
	//
	//	err := syscall.Fallocate(int(file.Fd()), FallocFlPunchHole, offset, length)
	//	if errno, ok := err.(syscall.Errno); ok {
	//		if errno == syscall.EOPNOTSUPP || errno == syscall.ENOSYS {
	//			log.Warnf("could not deallocate space, ignoring: %v", errno)
	//			err = nil // log and ignore
	//		}
	//	}
	//
	//	return err
}
