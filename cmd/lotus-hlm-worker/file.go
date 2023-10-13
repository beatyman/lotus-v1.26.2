package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gwaylib/errors"
)

const (
	append_file_new       = 0
	append_file_continue  = 1
	append_file_completed = 2
)

func checksumFile(aFile, bFile *os.File) (int, error) {
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	// checksum all data
	ah := sha1.New()
	if _, err := io.Copy(ah, aFile); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	aSum := ah.Sum(nil)

	bh := sha1.New()
	if _, err := io.Copy(bh, bFile); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	bSum := bh.Sum(nil)

	if !bytes.Equal(aSum, bSum) {
		return append_file_new, nil
	}

	return append_file_completed, nil
}
func canAppendFile(aFile, bFile *os.File, aStat, bStat os.FileInfo) (int, error) {
	checksumSize := int64(32 * 1024)
	// for small size, just do rewrite.
	aSize := aStat.Size()
	bSize := bStat.Size()
	if bSize < checksumSize {
		return append_file_new, nil
	}
	if bSize > aSize {
		return append_file_new, nil
	}

	// checksum the end
	aData := make([]byte, checksumSize)
	bData := make([]byte, checksumSize)
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	if _, err := aFile.ReadAt(aData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.ReadAt(bData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if !bytes.Equal(aData, bData) {
		return append_file_new, nil
	}
	if aSize > bSize {
		return append_file_continue, nil
	}

	return append_file_completed, nil
}

func travelFile(path string) (os.FileInfo, []string, error) {
	fStat, err := os.Lstat(path)
	if err != nil {
		return nil, nil, errors.As(err, path)
	}
	if !fStat.IsDir() {
		return nil, []string{path}, nil
	}
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, nil, errors.As(err)
	}
	result := []string{}
	for _, fs := range dirs {
		filePath := filepath.Join(path, fs.Name())
		if !fs.IsDir() {
			result = append(result, filePath)
			continue
		}
		_, nextFiles, err := travelFile(filePath)
		if err != nil {
			return nil, nil, errors.As(err, filePath)
		}
		result = append(result, nextFiles...)
	}
	return fStat, result, nil
}

func copyFile(ctx context.Context, from, to string) error {
	if from == to {
		return errors.New("Same file").As(from, to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return errors.As(err, to)
	}
	fromFile, err := os.Open(from)
	if err != nil {
		return errors.As(err, from)
	}
	defer fromFile.Close()
	fromStat, err := fromFile.Stat()
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(to)
		}
		return errors.As(err, to)
	}

	// TODO: make chtime
	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE, fromStat.Mode())
	if err != nil {
		return errors.As(err, to, uint64(fromStat.Mode()))
	}
	defer toFile.Close()
	toStat, err := toFile.Stat()
	if err != nil {
		return errors.As(err, to)
	}

	// checking continue
	stats, err := canAppendFile(fromFile, toFile, fromStat, toStat)
	if err != nil {
		return errors.As(err)
	}
	switch stats {
	case append_file_completed:
		// has done
		fmt.Printf("%s ======= completed\n", to)
		return nil

	case append_file_continue:
		appendPos := int64(toStat.Size() - 1)
		if appendPos < 0 {
			appendPos = 0
			fmt.Printf("%s ====== new \n", to)
		} else {
			fmt.Printf("%s ====== continue: %d\n", to, appendPos)
		}
		if _, err := fromFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
	default:
		fmt.Printf("%s ====== new \n", to)
		// TODO: allow truncate, current need to delete the files by manully.
		if err := toFile.Truncate(0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
		if _, err := fromFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
	}

	errBuff := make(chan error, 1)
	interrupt := false
	iLock := sync.Mutex{}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			iLock.Lock()
			if interrupt {
				iLock.Unlock()
				return
			}
			iLock.Unlock()

			nr, er := fromFile.Read(buf)
			if nr > 0 {
				nw, ew := toFile.Write(buf[0:nr])
				if ew != nil {
					errBuff <- errors.As(ew)
					return
				}
				if nr != nw {
					errBuff <- errors.As(io.ErrShortWrite)
					return
				}
			}
			if er != nil {
				errBuff <- errors.As(er)
				return
			}
		}
	}()
	select {
	case err := <-errBuff:
		if !errors.Equal(err, io.EOF) {
			return errors.As(err)
		}
		stats, err := checksumFile(fromFile, toFile)
		if err != nil {
			return errors.As(err)
		}
		if stats == append_file_completed {
			return nil
		}
		// TODO: allow truncate, current need to delete the files by manully.
		//if err := toFile.Truncate(0); err != nil {
		//	return errors.As(err, toFile)
		//}
		//if _, err := toFile.Seek(0, 0); err != nil {
		//	return errors.As(err, toFile)
		//}
		return errors.New("finalize has completed, but checksum failed.").As(stats, from, to, fromStat.Size(), toStat.Size())
	case <-ctx.Done():
		iLock.Lock()
		interrupt = true
		iLock.Unlock()
		return ctx.Err()
	}
}

func CopyFile(ctx context.Context, from, to string) error {
	_, source, err := travelFile(from)
	if err != nil {
		return errors.As(err)
	}
	for _, src := range source {
		toFile := strings.Replace(src, from, to, 1)
		tCtx, cancel := context.WithTimeout(ctx, time.Hour)
		if err := copyFile(tCtx, src, toFile); err != nil {
			cancel()
			log.Warn(errors.As(err))

			// try again
			tCtx, cancel = context.WithTimeout(ctx, time.Hour)
			if err := copyFile(tCtx, src, toFile); err != nil {
				cancel()
				return errors.As(err)
			}
		}
		cancel()
	}
	return nil
}