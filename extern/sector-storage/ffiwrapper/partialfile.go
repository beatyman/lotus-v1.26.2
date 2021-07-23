package ffiwrapper

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	rlepluslazy "github.com/filecoin-project/go-bitfield/rle"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gwaylib/errors"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/fsutil"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/specs-storage/storage"
)

const veryLargeRle = 1 << 20

// Sectors can be partially unsealed. We support this by appending a small
// trailer to each unsealed sector file containing an RLE+ marking which bytes
// in a sector are unsealed, and which are not (holes)

// unsealed sector files internally have this structure
// [unpadded (raw) data][rle+][4B LE length fo the rle+ field]

type partialFile struct {
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

func createUnsealedPartialFile(maxPieceSize abi.PaddedPieceSize, sector storage.SectorRef) (*partialFile, error) {
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

	return openUnsealedPartialFile(maxPieceSize, sector)
}

func openUnsealedPartialFile(maxPieceSize abi.PaddedPieceSize, sector storage.SectorRef) (*partialFile, error) {
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
		_ = f.Close()
		return nil, err
	}

	return &partialFile{
		maxPiece:  maxPieceSize,
		path:      path,
		allocated: rle,
		file:      f,
	}, nil
}

func (pf *partialFile) Close() error {
	return pf.file.Close()
}

func (pf *partialFile) Writer(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (io.Writer, error) {
	if _, err := pf.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seek piece start: %w", err)
	}

	{
		have, err := pf.allocated.RunIterator()
		if err != nil {
			return nil, err
		}

		and, err := rlepluslazy.And(have, pieceRun(offset, size))
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

func (pf *partialFile) MarkAllocated(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) error {
	have, err := pf.allocated.RunIterator()
	if err != nil {
		return err
	}

	ored, err := rlepluslazy.Or(have, pieceRun(offset, size))
	if err != nil {
		return err
	}

	if err := writeTrailer(int64(pf.maxPiece), pf.file, ored); err != nil {
		return xerrors.Errorf("writing trailer: %w", err)
	}

	return nil
}

func (pf *partialFile) Free(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) error {
	have, err := pf.allocated.RunIterator()
	if err != nil {
		return err
	}

	if err := fsutil.Deallocate(pf.file, int64(offset), int64(size)); err != nil {
		return xerrors.Errorf("deallocating: %w", err)
	}

	s, err := rlepluslazy.Subtract(have, pieceRun(offset, size))
	if err != nil {
		return err
	}

	if err := writeTrailer(int64(pf.maxPiece), pf.file, s); err != nil {
		return xerrors.Errorf("writing trailer: %w", err)
	}

	return nil
}

func (pf *partialFile) Reader(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (fsutil.PartialFile, error) {
	if _, err := pf.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seek piece start: %w", err)
	}

	{
		have, err := pf.allocated.RunIterator()
		if err != nil {
			return nil, err
		}

		and, err := rlepluslazy.And(have, pieceRun(offset, size))
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

	return pf.file, nil
}

func (pf *partialFile) Allocated() (rlepluslazy.RunIterator, error) {
	return pf.allocated.RunIterator()
}

func (pf *partialFile) HasAllocated(offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	have, err := pf.Allocated()
	if err != nil {
		return false, err
	}

	u, err := rlepluslazy.And(have, pieceRun(offset.Padded(), size.Padded()))
	if err != nil {
		return false, err
	}

	uc, err := rlepluslazy.Count(u)
	if err != nil {
		return false, err
	}

	return abi.PaddedPieceSize(uc) == size.Padded(), nil
}

func pieceRun(offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) rlepluslazy.RunIterator {
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