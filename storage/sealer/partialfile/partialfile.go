package partialfile

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/detailyang/go-fallocate"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	rlepluslazy "github.com/filecoin-project/go-bitfield/rle"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"

	"github.com/filecoin-project/lotus/lib/readerutil"
	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
)

var log = logging.Logger("partialfile")

const veryLargeRle = 1 << 20

// Sectors can be partially unsealed. We support this by appending a small
// trailer to each unsealed sector file containing an RLE+ marking which bytes
// in a sector are unsealed, and which are not (holes)

// unsealed sector files internally have this structure
// [unpadded (raw) data][rle+][4B LE length fo the rle+ field]

type PartialFile struct {
	maxPiece abi.PaddedPieceSize

	path      string
	allocated rlepluslazy.RLE

	file fsutil.PartialFile
}

func writeTrailer(maxPieceSize int64, w fsutil.PartialFile, r rlepluslazy.RunIterator) error {
	trailer, err := rlepluslazy.EncodeRuns(r, nil)
	if err != nil {
		return xerrors.Errorf("encoding trailer: %w", err)
	}

	// maxPieceSize == unpadded(sectorSize) == trailer start
	if _, err := w.Seek(maxPieceSize, io.SeekStart); err != nil {
		return xerrors.Errorf("seek to trailer start: %w", err)
	}

	rb, err := w.Write(trailer)
	if err != nil {
		return xerrors.Errorf("writing trailer data: %w", err)
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(len(trailer))); err != nil {
		return xerrors.Errorf("writing trailer length: %w", err)
	}

	return w.Truncate(maxPieceSize + int64(rb) + 4)
}

// fileExists checks if a file exists and is not a directory before we
// try using it to prevent further errors.
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
func CreateUnsealedPartialFile(maxPieceSize abi.PaddedPieceSize, sector storiface.SectorRef) (*PartialFile, error) {
	path := sector.UnsealedFile()
	var f fsutil.PartialFile
	switch sector.UnsealedStorageType {
	case database.MOUNT_TYPE_HLM:
		// loading special storage implement
		stor, err := database.GetStorage(sector.UnsealedStorageId)
		if err != nil {
			log.Warn(errors.As(err))
			return nil, errors.New(errors.As(err).Code())
		}
		sid := sector.SectorId
		auth := hlmclient.NewAuthClient(stor.MountAuthUri, stor.MountAuth)
		ctx := context.TODO()
		token, err := auth.NewFileToken(ctx, sid)
		if err != nil {
			log.Warn(errors.As(err))
			return nil, errors.New(errors.As(err).Code())
		}
		f = hlmclient.OpenHttpFile(ctx, stor.MountTransfUri, filepath.Join("unsealed", sid), sid, string(token))
	default:
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, xerrors.Errorf("creating file '%s': %w", path, err)
		}
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644) // nolint
		if err != nil {
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
		f = file
	}

	err := func() error {
		//err := fallocate.Fallocate(f, 0, int64(maxPieceSize))
		//if errno, ok := err.(syscall.Errno); ok {
		//	if errno == syscall.EOPNOTSUPP || errno == syscall.ENOSYS {
		//		log.Warnf("could not allocated space, ignoring: %v", errno)
		//		err = nil // log and ignore
		//	}
		//}
		//if err != nil {
		//	return xerrors.Errorf("fallocate '%s': %w", path, err)
		//}

		if err := writeTrailer(int64(maxPieceSize), f, &rlepluslazy.RunSliceIterator{}); err != nil {
			return xerrors.Errorf("writing trailer: %w", err)
		}

		return nil
	}()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, xerrors.Errorf("close empty partial file: %w", err)
	}

	return OpenUnsealedPartialFile(maxPieceSize, sector)
}

func OpenUnsealedPartialFile(maxPieceSize abi.PaddedPieceSize, sector storiface.SectorRef) (*PartialFile, error) {
	path := sector.UnsealedFile()
	var f fsutil.PartialFile
	switch sector.UnsealedStorageType {
	case database.MOUNT_TYPE_HLM:
		// loading special storage implement
		stor, err := database.GetStorage(sector.UnsealedStorageId)
		if err != nil {
			log.Warn(errors.As(err))
			return nil, errors.New(errors.As(err).Code())
		}
		sid := sector.SectorId
		auth := hlmclient.NewAuthClient(stor.MountAuthUri, stor.MountAuth)
		ctx := context.TODO()
		token, err := auth.NewFileToken(ctx, sid)
		if err != nil {
			log.Warn(errors.As(err))
			return nil, errors.New(errors.As(err).Code())
		}
		f = hlmclient.OpenHttpFile(ctx, stor.MountTransfUri, filepath.Join("unsealed", sid), sid, string(token))
		// need the file has exist.
		if _, err := f.Stat(); err != nil {
			log.Warn(errors.As(err))
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
	case database.MOUNT_TYPE_OSS:
		if fileExists(path) {
			f2, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
			if err != nil {
				return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
			}
			f = f2
		} else {
			dir, _ := filepath.Split(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Warnf("MkdirAll err: %s", err)
			}
			d := operation.NewDownloaderV2()
			f2, err := d.DownloadFile(path, path)
			if err != nil {
				return nil, xerrors.Errorf("download partial file '%s': %w", path, err)
			}
			f = f2
		}
	default:
		osfile, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
		if err != nil {
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
		f = osfile
	}

	var rle rlepluslazy.RLE
	err := func() error {
		st, err := f.Stat()
		if err != nil {
			return xerrors.Errorf("stat '%s': %w", path, err)
		}
		if st.Size() < int64(maxPieceSize) {
			return xerrors.Errorf("sector file '%s' was smaller than the sector size %d < %d", path, st.Size(), maxPieceSize)
		}
		// read trailer
		var tlen [4]byte
		_, err = f.ReadAt(tlen[:], st.Size()-int64(len(tlen)))
		if err != nil {
			return xerrors.Errorf("reading trailer length: %w", err)
		}

		// sanity-check the length
		trailerLen := binary.LittleEndian.Uint32(tlen[:])
		expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
		if expectLen != st.Size() {
			return xerrors.Errorf("file '%s' has inconsistent length; has %d bytes; expected %d (%d trailer, %d sector data)", path, st.Size(), expectLen, int64(trailerLen)+int64(len(tlen)), maxPieceSize)
		}
		if trailerLen > veryLargeRle {
			log.Warnf("Partial file '%s' has a VERY large trailer with %d bytes", path, trailerLen)
		}

		trailerStart := st.Size() - int64(len(tlen)) - int64(trailerLen)
		if trailerStart != int64(maxPieceSize) {
			return xerrors.Errorf("expected sector size to equal trailer start index")
		}

		trailerBytes := make([]byte, trailerLen)
		_, err = f.ReadAt(trailerBytes, trailerStart)
		if err != nil {
			return xerrors.Errorf("reading trailer: %w", err)
		}

		rle, err = rlepluslazy.FromBuf(trailerBytes)
		if err != nil {
			return xerrors.Errorf("decoding trailer: %w", err)
		}

		it, err := rle.RunIterator()
		if err != nil {
			return xerrors.Errorf("getting trailer run iterator: %w", err)
		}

		f, err := rlepluslazy.Fill(it)
		if err != nil {
			return xerrors.Errorf("filling bitfield: %w", err)
		}
		lastSet, err := rlepluslazy.Count(f)
		if err != nil {
			return xerrors.Errorf("finding last set byte index: %w", err)
		}

		if lastSet > uint64(maxPieceSize) {
			return xerrors.Errorf("last set byte at index higher than sector size: %d > %d", lastSet, maxPieceSize)
		}

		return nil
	}()
	if err != nil {
		log.Error(err)
		_ = f.Close()
		return nil, err
	}

	return &PartialFile{
		maxPiece:  maxPieceSize,
		path:      path,
		allocated: rle,
		file:      f,
	}, nil
}
func OpenUnsealedPartialFileV2(maxPieceSize abi.PaddedPieceSize, sector storiface.SectorRef, ss database.SectorStorage) (*PartialFile, error) {
	log.Infof("%+v", sector)
	//分开部署时候需要mount
	sectorName := storiface.SectorName(sector.ID)
	path := filepath.Join(ss.UnsealedStorage.MountDir, fmt.Sprintf("%d", ss.UnsealedStorage.ID), "unsealed", sectorName)
	//path := sector.UnsealedFile()
	if ss.UnsealedStorage.MountType == database.MOUNT_TYPE_OSS {
		sp := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, storiface.SectorName(sector.ID))
		path = filepath.Join(sp, storiface.FTUnsealed.String(), storiface.SectorName(sector.ID))
	}
	log.Info(path)
	var f fsutil.PartialFile
	switch ss.UnsealedStorage.MountType {
	case database.MOUNT_TYPE_HLM:
		// loading special storage implement
		log.Info("hlm OpenUnsealedPartialFileV2")
		sid := ss.SectorInfo.ID
		auth := hlmclient.NewAuthClient(ss.UnsealedStorage.MountAuthUri, ss.UnsealedStorage.MountAuth)
		ctx := context.TODO()
		token, err := auth.NewFileToken(ctx, sid)
		if err != nil {
			log.Warn(errors.As(err))
			return nil, errors.New(errors.As(err).Code())
		}
		log.Info("hlmclient.OpenHttpFile")
		f = hlmclient.OpenHttpFile(ctx, ss.UnsealedStorage.MountTransfUri, filepath.Join("unsealed", sid), sid, string(token))
		// need the file has exist.
		if _, err := f.Stat(); err != nil {
			log.Warn(errors.As(err))
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
	case database.MOUNT_TYPE_OSS:
		if fileExists(path) {
			f2, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
			if err != nil {
				return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
			}
			f = f2
		} else {
			dir, _ := filepath.Split(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Warnf("MkdirAll err: %s", err)
			}
			d := operation.NewDownloaderV2()
			f2, err := d.DownloadFile(path, path)
			if err != nil {
				return nil, xerrors.Errorf("download partial file '%s': %w", path, err)
			}
			f = f2
		}
	case database.MOUNT_TYPE_FCFS:
		//手动自己mount
		osfile, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
		if err != nil {
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
		f = osfile
	case database.MOUNT_TYPE_UFILE:
		//手动自己mount
		osfile, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
		if err != nil {
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
		f = osfile
	default:
		if err := database.Mount(
			context.TODO(),
			ss.UnsealedStorage.MountType,
			ss.UnsealedStorage.MountSignalUri,
			filepath.Join(ss.UnsealedStorage.MountDir, fmt.Sprintf("%d", ss.UnsealedStorage.ID)),
			ss.UnsealedStorage.MountOpt,
		); err != nil {
			return nil, err
		}
		osfile, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
		if err != nil {
			return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
		}
		f = osfile
	}

	var rle rlepluslazy.RLE
	err := func() error {
		st, err := f.Stat()
		if err != nil {
			return xerrors.Errorf("stat '%s': %w", path, err)
		}
		if st.Size() < int64(maxPieceSize) {
			return xerrors.Errorf("sector file '%s' was smaller than the sector size %d < %d", path, st.Size(), maxPieceSize)
		}
		// read trailer
		var tlen [4]byte
		_, err = f.ReadAt(tlen[:], st.Size()-int64(len(tlen)))
		if err != nil {
			return xerrors.Errorf("reading trailer length: %w", err)
		}

		// sanity-check the length
		trailerLen := binary.LittleEndian.Uint32(tlen[:])
		expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
		if expectLen != st.Size() {
			return xerrors.Errorf("file '%s' has inconsistent length; has %d bytes; expected %d (%d trailer, %d sector data)", path, st.Size(), expectLen, int64(trailerLen)+int64(len(tlen)), maxPieceSize)
		}
		if trailerLen > veryLargeRle {
			log.Warnf("Partial file '%s' has a VERY large trailer with %d bytes", path, trailerLen)
		}

		trailerStart := st.Size() - int64(len(tlen)) - int64(trailerLen)
		if trailerStart != int64(maxPieceSize) {
			return xerrors.Errorf("expected sector size to equal trailer start index")
		}

		trailerBytes := make([]byte, trailerLen)
		_, err = f.ReadAt(trailerBytes, trailerStart)
		if err != nil {
			return xerrors.Errorf("reading trailer: %w", err)
		}

		rle, err = rlepluslazy.FromBuf(trailerBytes)
		if err != nil {
			return xerrors.Errorf("decoding trailer: %w", err)
		}

		it, err := rle.RunIterator()
		if err != nil {
			return xerrors.Errorf("getting trailer run iterator: %w", err)
		}

		f, err := rlepluslazy.Fill(it)
		if err != nil {
			return xerrors.Errorf("filling bitfield: %w", err)
		}
		lastSet, err := rlepluslazy.Count(f)
		if err != nil {
			return xerrors.Errorf("finding last set byte index: %w", err)
		}

		if lastSet > uint64(maxPieceSize) {
			return xerrors.Errorf("last set byte at index higher than sector size: %d > %d", lastSet, maxPieceSize)
		}

		return nil
	}()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &PartialFile{
		maxPiece:  maxPieceSize,
		path:      path,
		allocated: rle,
		file:      f,
	}, nil
}

func (pf *PartialFile) Close() error {
	if pf.file != nil {
		return pf.file.Close()
	}
	return nil
}

func (pf *PartialFile) Writer(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (io.Writer, error) {
	if _, err := pf.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seek piece start: %w", err)
	}

	{
		have, err := pf.allocated.RunIterator()
		if err != nil {
			return nil, err
		}

		and, err := rlepluslazy.And(have, PieceRun(offset, size))
		if err != nil {
			return nil, err
		}

		c, err := rlepluslazy.Count(and)
		if err != nil {
			return nil, err
		}

		if c > 0 {
			log.Warnf("getting partial file writer overwriting %d allocated bytes", c)
		}
	}

	return pf.file, nil
}

func (pf *PartialFile) MarkAllocated(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) error {
	have, err := pf.allocated.RunIterator()
	if err != nil {
		return err
	}

	ored, err := rlepluslazy.Or(have, PieceRun(offset, size))
	if err != nil {
		return err
	}

	if err := writeTrailer(int64(pf.maxPiece), pf.file, ored); err != nil {
		return xerrors.Errorf("writing trailer: %w", err)
	}

	return nil
}

func (pf *PartialFile) Free(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) error {
	have, err := pf.allocated.RunIterator()
	if err != nil {
		return err
	}

	if err := fsutil.Deallocate(pf.file, int64(offset), int64(size)); err != nil {
		return xerrors.Errorf("deallocating: %w", err)
	}

	s, err := rlepluslazy.Subtract(have, PieceRun(offset, size))
	if err != nil {
		return err
	}

	if err := writeTrailer(int64(pf.maxPiece), pf.file, s); err != nil {
		return xerrors.Errorf("writing trailer: %w", err)
	}

	return nil
}

// Reader forks off a new reader from the underlying file, and returns a reader
// starting at the given offset and reading the given size. Safe for concurrent
// use.
func (pf *PartialFile) Reader(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (fsutil.PartialFile, error) {
	if _, err := pf.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seek piece start: %w", err)
	}

	{
		have, err := pf.allocated.RunIterator()
		if err != nil {
			return nil, err
		}

		and, err := rlepluslazy.And(have, PieceRun(offset, size))
		if err != nil {
			return nil, err
		}

		c, err := rlepluslazy.Count(and)
		if err != nil {
			return nil, err
		}

		if c != uint64(size) {
			log.Warnf("getting partial file reader reading %d unallocated bytes", uint64(size)-c)
		}
	}

	return io.LimitReader(readerutil.NewReadSeekerFromReaderAt(pf.file, int64(offset)), int64(size)), nil
}

func (pf *PartialFile) Allocated() (rlepluslazy.RunIterator, error) {
	return pf.allocated.RunIterator()
}

func (pf *PartialFile) HasAllocated(offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	have, err := pf.Allocated()
	if err != nil {
		return false, err
	}

	u, err := rlepluslazy.And(have, PieceRun(offset.Padded(), size.Padded()))
	if err != nil {
		return false, err
	}

	uc, err := rlepluslazy.Count(u)
	if err != nil {
		return false, err
	}

	return abi.PaddedPieceSize(uc) == size.Padded(), nil
}

func PieceRun(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) rlepluslazy.RunIterator {
	var runs []rlepluslazy.Run
	if offset > 0 {
		runs = append(runs, rlepluslazy.Run{
			Val: false,
			Len: uint64(offset),
		})
	}

	runs = append(runs, rlepluslazy.Run{
		Val: true,
		Len: uint64(size),
	})

	return &rlepluslazy.RunSliceIterator{Runs: runs}
}
func CreatePartialFile(maxPieceSize abi.PaddedPieceSize, path string) (*PartialFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644) // nolint
	if err != nil {
		return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
	}

	err = func() error {
		err := fallocate.Fallocate(f, 0, int64(maxPieceSize))
		if errno, ok := err.(syscall.Errno); ok {
			if errno == syscall.EOPNOTSUPP || errno == syscall.ENOSYS {
				log.Warnf("could not allocate space, ignoring: %v", errno)
				err = nil // log and ignore
			}
		}
		if err != nil {
			return xerrors.Errorf("fallocate '%s': %w", path, err)
		}

		if err := writeTrailer(int64(maxPieceSize), f, &rlepluslazy.RunSliceIterator{}); err != nil {
			return xerrors.Errorf("writing trailer: %w", err)
		}

		return nil
	}()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, xerrors.Errorf("close empty partial file: %w", err)
	}

	return OpenPartialFile(maxPieceSize, path)
}

func OpenPartialFile(maxPieceSize abi.PaddedPieceSize, path string) (*PartialFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0644) // nolint
	if err != nil {
		return nil, xerrors.Errorf("openning partial file '%s': %w", path, err)
	}

	var rle rlepluslazy.RLE
	err = func() error {
		st, err := f.Stat()
		if err != nil {
			return xerrors.Errorf("stat '%s': %w", path, err)
		}
		if st.Size() < int64(maxPieceSize) {
			return xerrors.Errorf("sector file '%s' was smaller than the sector size %d < %d", path, st.Size(), maxPieceSize)
		}
		// read trailer
		var tlen [4]byte
		_, err = f.ReadAt(tlen[:], st.Size()-int64(len(tlen)))
		if err != nil {
			return xerrors.Errorf("reading trailer length: %w", err)
		}

		// sanity-check the length
		trailerLen := binary.LittleEndian.Uint32(tlen[:])
		expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
		if expectLen != st.Size() {
			return xerrors.Errorf("file '%s' has inconsistent length; has %d bytes; expected %d (%d trailer, %d sector data)", path, st.Size(), expectLen, int64(trailerLen)+int64(len(tlen)), maxPieceSize)
		}
		if trailerLen > veryLargeRle {
			log.Warnf("Partial file '%s' has a VERY large trailer with %d bytes", path, trailerLen)
		}

		trailerStart := st.Size() - int64(len(tlen)) - int64(trailerLen)
		if trailerStart != int64(maxPieceSize) {
			return xerrors.Errorf("expected sector size to equal trailer start index")
		}

		trailerBytes := make([]byte, trailerLen)
		_, err = f.ReadAt(trailerBytes, trailerStart)
		if err != nil {
			return xerrors.Errorf("reading trailer: %w", err)
		}

		rle, err = rlepluslazy.FromBuf(trailerBytes)
		if err != nil {
			return xerrors.Errorf("decoding trailer: %w", err)
		}

		it, err := rle.RunIterator()
		if err != nil {
			return xerrors.Errorf("getting trailer run iterator: %w", err)
		}

		f, err := rlepluslazy.Fill(it)
		if err != nil {
			return xerrors.Errorf("filling bitfield: %w", err)
		}
		lastSet, err := rlepluslazy.Count(f)
		if err != nil {
			return xerrors.Errorf("finding last set byte index: %w", err)
		}

		if lastSet > uint64(maxPieceSize) {
			return xerrors.Errorf("last set byte at index higher than sector size: %d > %d", lastSet, maxPieceSize)
		}

		return nil
	}()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &PartialFile{
		maxPiece:  maxPieceSize,
		path:      path,
		allocated: rle,
		file:      f,
	}, nil
}
