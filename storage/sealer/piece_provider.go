package sealer

import (
	"bufio"
	"context"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/partialfile"
	"github.com/gwaylib/errors"
	"golang.org/x/xerrors"
	"io"
	"os"

	"github.com/filecoin-project/dagstore/mount"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/lotus/storage/paths"
	"github.com/filecoin-project/lotus/storage/sealer/fr32"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type Unsealer interface {
	// SectorsUnsealPiece will Unseal a Sealed sector file for the given sector.
	SectorsUnsealPiece(ctx context.Context, sector storiface.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, commd *cid.Cid) error
	//ReadPiece(ctx context.Context, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error)
	ReadPieceStorageInfo(ctx context.Context, sector storiface.SectorRef) (database.SectorStorage, error)
}

type PieceProvider interface {
	// ReadPiece is used to read an Unsealed piece at the given offset and of the given size from a Sector
	// pieceOffset + pieceSize specify piece bounds for unsealing (note: with SDR the entire sector will be unsealed by
	//  default in most cases, but this might matter with future PoRep)
	// startOffset is added to the pieceOffset to get the starting reader offset.
	// The number of bytes that can be read is pieceSize-startOffset
	ReadPiece(ctx context.Context, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, pieceSize abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error)
	IsUnsealed(ctx context.Context, sector storiface.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error)
}

var _ PieceProvider = &pieceProvider{}

type pieceProvider struct {
	storage *paths.Remote
	index   paths.SectorIndex
	uns     Unsealer
}

func NewPieceProvider(storage *paths.Remote, index paths.SectorIndex, uns Unsealer) PieceProvider {
	return &pieceProvider{
		storage: storage,
		index:   index,
		uns:     uns,
	}
}

// IsUnsealed checks if we have the unsealed piece at the given offset in an already
// existing unsealed file either locally or on any of the workers.
func (p *pieceProvider) IsUnsealed(ctx context.Context, sector storiface.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	if err := offset.Valid(); err != nil {
		return false, xerrors.Errorf("offset is not valid: %w", err)
	}
	if err := size.Validate(); err != nil {
		return false, xerrors.Errorf("size is not a valid piece size: %w", err)
	}
	info, err := p.uns.ReadPieceStorageInfo(ctx, sector)
	if err != nil {
		log.Error(err)
		return false,err
	} else {
		log.Infof("%+v", info)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return false,err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)
	log.Info("Start OpenUnsealedPartialFileV2")
	_, err = partialfile.OpenUnsealedPartialFileV2(maxPieceSize, sector, info)
	if err != nil {
		log.Error(err)
		return false,err
	}
	return true,nil
	ctxLock, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := p.index.StorageLock(ctxLock, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		return false, xerrors.Errorf("acquiring read sector lock: %w", err)
	}

	return p.storage.CheckIsUnsealed(ctxLock, sector, abi.PaddedPieceSize(offset.Padded()), size.Padded())
}

// tryReadUnsealedPiece will try to read the unsealed piece from an existing unsealed sector file for the given sector from any worker that has it.
// It will NOT try to schedule an Unseal of a sealed sector file for the read.
//
// Returns a nil reader if the piece does NOT exist in any unsealed file or there is no unsealed file for the given sector on any of the workers.
func (p *pieceProvider) tryReadUnsealedPiece(ctx context.Context, pc cid.Cid, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (mount.Reader, error) {
	// acquire a lock purely for reading unsealed sectors
	ctx, cancel := context.WithCancel(ctx)
	if err := p.index.StorageLock(ctx, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		cancel()
		return nil, xerrors.Errorf("acquiring read sector lock: %w", err)
	}

	// Reader returns a reader getter for an unsealed piece at the given offset in the given sector.
	// The returned reader will be nil if none of the workers has an unsealed sector file containing
	// the unsealed piece.
	rg, err := p.storage.Reader(ctx, sector, abi.PaddedPieceSize(pieceOffset.Padded()), size.Padded())
	if err != nil {
		cancel()
		log.Debugf("did not get storage reader;sector=%+v, err:%s", sector.ID, err)
		return nil, err
	}
	if rg == nil {
		cancel()
		return nil, nil
	}

	buf := make([]byte, fr32.BufSize(size.Padded()))

	pr, err := (&pieceReader{
		ctx: ctx,
		getReader: func(ctx context.Context, startOffset uint64) (io.ReadCloser, error) {
			startOffsetAligned := storiface.UnpaddedByteIndex(startOffset / 127 * 127) // floor to multiple of 127

			r, err := rg(startOffsetAligned.Padded())
			if err != nil {
				return nil, xerrors.Errorf("getting reader at +%d: %w", startOffsetAligned, err)
			}

			upr, err := fr32.NewUnpadReaderBuf(r, size.Padded(), buf)
			if err != nil {
				r.Close() // nolint
				return nil, xerrors.Errorf("creating unpadded reader: %w", err)
			}

			bir := bufio.NewReaderSize(upr, 127)
			if startOffset > uint64(startOffsetAligned) {
				if _, err := bir.Discard(int(startOffset - uint64(startOffsetAligned))); err != nil {
					r.Close() // nolint
					return nil, xerrors.Errorf("discarding bytes for startOffset: %w", err)
				}
			}

			return struct {
				io.Reader
				io.Closer
			}{
				Reader: bir,
				Closer: funcCloser(func() error {
					return r.Close()
				}),
			}, nil
		},
		len:      size,
		onClose:  cancel,
		pieceCid: pc,
	}).init()
	if err != nil || pr == nil { // pr == nil to make sure we don't return typed nil
		cancel()
		return nil, err
	}

	return pr, err
}

type funcCloser func() error

func (f funcCloser) Close() error {
	return f()
}

var _ io.Closer = funcCloser(nil)

// ReadPiece is used to read an Unsealed piece at the given offset and of the given size from a Sector
// If an Unsealed sector file exists with the Piece Unsealed in it, we'll use that for the read.
// Otherwise, we will Unseal a Sealed sector file for the given sector and read the Unsealed piece from it.
// If we do NOT have an existing unsealed file  containing the given piece thus causing us to schedule an Unseal,
// the returned boolean parameter will be set to true.
// If we have an existing unsealed file containing the given piece, the returned boolean will be set to false.
func (p *pieceProvider) ReadPiece(ctx context.Context, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error) {
	if err := pieceOffset.Valid(); err != nil {
		return nil, false, xerrors.Errorf("pieceOffset is not valid: %w", err)
	}
	if err := size.Validate(); err != nil {
		return nil, false, xerrors.Errorf("size is not a valid piece size: %w", err)
	}
	return p.readPiece(ctx, sector, pieceOffset, size, ticket, unsealed)
	r, err := p.tryReadUnsealedPiece(ctx, unsealed, sector, pieceOffset, size)

	if xerrors.Is(err, storiface.ErrSectorNotFound) {
		log.Debugf("no unsealed sector file with unsealed piece, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, size)
		err = nil
	}
	if err != nil {
		log.Errorf("returning error from ReadPiece:%s", err)
		return nil, false, err
	}

	var uns bool

	if r == nil {
		// a nil reader means that none of the workers has an unsealed sector file
		// containing the unsealed piece.
		// we now need to unseal a sealed sector file for the given sector to read the unsealed piece from it.
		uns = true
		commd := &unsealed
		if unsealed == cid.Undef {
			commd = nil
		}
		if err := p.uns.SectorsUnsealPiece(ctx, sector, pieceOffset, size, ticket, commd); err != nil {
			log.Errorf("failed to SectorsUnsealPiece: %s", err)
			return nil, false, xerrors.Errorf("unsealing piece: %w", err)
		}

		log.Debugf("unsealed a sector file to read the piece, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, size)

		r, err = p.tryReadUnsealedPiece(ctx, unsealed, sector, pieceOffset, size)
		if err != nil {
			log.Errorf("failed to tryReadUnsealedPiece after SectorsUnsealPiece: %s", err)
			return nil, true, xerrors.Errorf("read after unsealing: %w", err)
		}
		if r == nil {
			log.Errorf("got no reader after unsealing piece")
			return nil, true, xerrors.Errorf("got no reader after unsealing piece")
		}
		log.Debugf("got a reader to read unsealed piece, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, size)
	} else {
		log.Debugf("unsealed piece already exists, no need to unseal, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, size)
	}

	log.Debugf("returning reader to read unsealed piece, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, size)

	return r, uns, nil
}
func (p *pieceProvider) PieceReader(ctx context.Context, sector storiface.SectorRef, offset, size abi.PaddedPieceSize) (func(startOffsetAligned storiface.PaddedByteIndex) (io.ReadCloser, error), bool, error) {
	log.Infof("DEBUG:PieceReader in, sector:%+v", sector)
	defer log.Infof("DEBUG:PieceReader out, sector:%+v", sector)
	// uprade SectorRef
	log.Info("Try ReadPiece Start")
	info, err := p.uns.ReadPieceStorageInfo(ctx, sector)
	if err != nil {
		log.Error(err)
		return nil, false, err
	} else {
		log.Infof("%+v", info)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, false, err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)
	log.Info("Start OpenUnsealedPartialFileV2")
	var pf *partialfile.PartialFile
	if info.UnsealedStorage.MountType == database.MOUNT_TYPE_OSS {
		up := os.Getenv("US3")
		if up != "" {
			pf, err = partialfile.OpenUnsealedPartialFileV2(maxPieceSize, sector, info)
			if err != nil {
				log.Error(err)
				if xerrors.Is(err, os.ErrNotExist) {
					return nil, false, nil
				}
				return nil, false, xerrors.Errorf("opening partial file: %w", err)
			}
		} else {
			return nil, false, xerrors.Errorf("opening partial file: %w", err)
		}
		log.Info("Over OpenUnsealedPartialFileV2")
	} else {
		pf, err = partialfile.OpenUnsealedPartialFileV2(maxPieceSize, sector, info)
		if err != nil {
			log.Error(err)
			if xerrors.Is(err, os.ErrNotExist) {
				return nil, false, nil
			}
			return nil, false, xerrors.Errorf("opening partial file: %w", err)
		}
	}

	ok, err := pf.HasAllocated(storiface.UnpaddedByteIndex(offset.Unpadded()), size.Unpadded())
	if err != nil {
		log.Error(err)
		_ = pf.Close()
		return nil, false, err
	}
	if !ok {
		log.Info("!ok: =============== ")
		_ = pf.Close()
		return nil, false, nil
	}
	return func(startOffsetAligned storiface.PaddedByteIndex) (io.ReadCloser, error) {
		r, err := pf.Reader(storiface.PaddedByteIndex(offset)+startOffsetAligned, size-abi.PaddedPieceSize(startOffsetAligned))
		if err != nil {
			log.Error(err)
			_ = pf.Close()
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{
			Reader: r,
			Closer: funcCloser(func() error {
				log.Info("close ====================================")
				// if we already have a reader cached, close this one
				//_ = pf.Close()
				return nil
			}),
		}, nil
	}, true, nil
}
func (p *pieceProvider) readPiece(ctx context.Context, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error) {
	// try read the exist unsealed.
	ctx, cancel := context.WithCancel(ctx)
	rg, done, err := p.PieceReader(ctx, sector, abi.PaddedPieceSize(pieceOffset.Padded()), size.Padded())
	if err != nil {
		log.Error(err)
		cancel()
		return nil, false, errors.As(err)
	} else if done {
		log.Infof("done: ================== %+v", done)
		cancel()
		buf := make([]byte, fr32.BufSize(size.Padded()))
		pr, err := (&pieceReader{
			ctx: ctx,
			getReader: func(ctx context.Context, startOffset uint64) (io.ReadCloser, error) {
				startOffsetAligned := storiface.UnpaddedByteIndex(startOffset / 127 * 127) // floor to multiple of 127

				r, err := rg(startOffsetAligned.Padded())
				if err != nil {
					return nil, xerrors.Errorf("getting reader at +%d: %w", startOffsetAligned, err)
				}

				upr, err := fr32.NewUnpadReaderBuf(r, size.Padded(), buf)
				if err != nil {
					r.Close() // nolint
					return nil, xerrors.Errorf("creating unpadded reader: %w", err)
				}

				bir := bufio.NewReaderSize(upr, 127)
				if startOffset > uint64(startOffsetAligned) {
					if _, err := bir.Discard(int(startOffset - uint64(startOffsetAligned))); err != nil {
						r.Close() // nolint
						return nil, xerrors.Errorf("discarding bytes for startOffset: %w", err)
					}
				}
				return struct {
					io.Reader
					io.Closer
				}{
					Reader: bir,
					Closer: funcCloser(func() error {
						return r.Close()
					}),
				}, nil
			},
			len:      size,
			onClose:  cancel,
			pieceCid: unsealed,
		}).init()
		if err != nil || pr == nil { // pr == nil to make sure we don't return typed nil
			cancel()
			return nil, true, err
		}
		cancel()
		return pr, true, nil
	}
	cancel()
	return nil, false, errors.New("readPiece not done")
}