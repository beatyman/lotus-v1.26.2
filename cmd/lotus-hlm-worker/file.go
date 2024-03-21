package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"github.com/gwaylib/errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	append_file_new       = 0
	append_file_continue  = 1
	append_file_completed = 2
)
const (
	_HASH_KIND_NONE  = 0 // no hash
	_HASH_KIND_QUICK = 1 // quick hash
	_HASH_KIND_DEEP  = 2 // deep hash
)

func checksumFile(aPath, bPath string, hashKind int) error {
	aFile, err := os.Open(aPath)
	if err != nil {
		return errors.As(err, aPath)
	}
	defer aFile.Close()
	aStat, err := aFile.Stat()
	if err != nil {
		return errors.As(err, aPath)
	}
	aSize := aStat.Size()

	bFile, err := os.Open(bPath)
	if err != nil {
		if os.IsNotExist(err) && aSize == 0 {
			return nil
		}
		return errors.As(err, bPath)
	}
	defer bFile.Close()
	bStat, err := bFile.Stat()
	if err != nil {
		return errors.As(err, bPath)
	}
	bSize := bStat.Size()
	if bSize == 0 {
		if aSize == 0 {
			return nil
		}
		return errors.As(os.ErrNotExist, bPath)
	}

	if aSize != bSize {
		return errors.New("size not match").As(aPath, aStat.Size(), bPath, bStat.Size())
	}

	switch hashKind {
	case _HASH_KIND_QUICK:
		aBuf := make([]byte, 4*1024) // 4k
		bBuf := make([]byte, 4*1024) // 4k
		if aStat.Size() < 4*1024 {
			if _, err := aFile.ReadAt(aBuf[:aSize], 0); err != nil {
				return errors.As(err, aPath)
			}
			if _, err := bFile.ReadAt(bBuf[:bSize], 0); err != nil {
				return errors.As(err, bPath)
			}
		} else {
			// section 1
			if _, err := aFile.ReadAt(aBuf[:1024], 0); err != nil {
				return errors.As(err, aPath)
			}
			if _, err := bFile.ReadAt(bBuf[:1024], 0); err != nil {
				return errors.As(err, bPath)
			}
			// section 2
			if _, err := aFile.ReadAt(aBuf[1024:3*1024], aSize/2-1024); err != nil {
				return errors.As(err, aPath)
			}
			if _, err := bFile.ReadAt(bBuf[1024:3*1024], bSize/2-1024); err != nil {
				return errors.As(err, bPath)
			}
			// section 3
			if _, err := aFile.ReadAt(aBuf[3*1024:4*1024], aSize-1024); err != nil {
				return errors.As(err, aPath)
			}
			if _, err := bFile.ReadAt(bBuf[3*1024:4*1024], bSize-1024); err != nil {
				return errors.As(err, bPath)
			}
		}
		if bytes.Compare(aBuf, bBuf) != 0 {
			return errors.New("4k checksum not match").As(aPath, bPath)
		}
	case _HASH_KIND_DEEP:
		// checksum all data
		if _, err := aFile.Seek(0, 0); err != nil {
			return errors.As(err, aPath)
		}
		if _, err := bFile.Seek(0, 0); err != nil {
			return errors.As(err, bPath)
		}

		ah := md5.New()
		if _, err := io.Copy(ah, aFile); err != nil {
			return errors.As(err, aFile.Name())
		}
		aSum := ah.Sum(nil)

		bh := md5.New()
		if _, err := io.Copy(bh, bFile); err != nil {
			return errors.As(err, bFile.Name())
		}
		bSum := bh.Sum(nil)

		if !bytes.Equal(aSum, bSum) {
			return errors.New("md5sum not match").As(fmt.Sprintf("%x,%x", aSum, bSum))
		}

	}
	return nil
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

func travelFileEmpty(path string) (os.FileInfo, []string, error) {
	return nil, []string{path}, nil
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
	fromStat, err := os.Stat(from)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(to)
		}
		return errors.As(err, to)
	}
	if fromStat.Size() == 0 {
		log.Infof("ignore no size file:%s", from)
		return nil
	}
	if _, err := os.Stat(filepath.Dir(to)); err != nil {
		if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			log.Errorf("mkdir err :%s", to)
			return errors.As(err, to)
		}
	}
	if strings.Contains(from, "comm_d") ||
		strings.Contains(from, "sealed-file") ||
		strings.Contains(from, "comm_r") ||
		strings.Contains(from, "p1.out") ||
		strings.Contains(from, "c1.out") ||
		strings.Contains(from, "c2.out") ||
		strings.Contains(from, "data-layer") ||
		strings.Contains(from, "tree-d") ||
		strings.Contains(from, "tree-c") {
		log.Infof("ignore local file:%s", from)
		return nil
	}
	// use the system command because some storage is more suitable
	cmd := exec.CommandContext(ctx, "/usr/bin/env", "cp", "-vf", from, to)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.As(err)
	}
	if err := checksumFile(from, to, _HASH_KIND_QUICK); err != nil {
		return errors.As(err)
	}
	return nil
}
