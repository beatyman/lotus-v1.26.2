package sectorstorage

import (
	"bufio"
	"context"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/partialfile"
	"io"
	"os"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/lotus/extern/sector-storage/fr32"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
)

type Unsealer interface {
	// SectorsUnsealPiece will Unseal a Sealed sector file for the given sector.
	SectorsUnsealPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, commd *cid.Cid) error
	//ReadPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (io.ReadCloser, bool, error)
	ReadPieceStorageInfo(ctx context.Context, sector storage.SectorRef) (database.SectorStorage, error)
}

type PieceProvider interface {
	// ReadPiece is used to read an Unsealed piece at the given offset and of the given size from a Sector
	ReadPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (io.ReadCloser, bool, error)
	IsUnsealed(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error)
}

var _ PieceProvider = &pieceProvider{}

type pieceProvider struct {
	storage *stores.Remote
	index   stores.SectorIndex
	uns     Unsealer
}

func NewPieceProvider(storage *stores.Remote, index stores.SectorIndex, uns Unsealer) PieceProvider {
	return &pieceProvider{
		storage: storage,
		index:   index,
		uns:     uns,
	}
}

// IsUnsealed checks if we have the unsealed piece at the given offset in an already
// existing unsealed file either locally or on any of the workers.
func (p *pieceProvider) IsUnsealed(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	if err := offset.Valid(); err != nil {
		return false, xerrors.Errorf("offset is not valid: %w", err)
	}
	if err := size.Validate(); err != nil {
		return false, xerrors.Errorf("size is not a valid piece size: %w", err)
	}

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
func (p *pieceProvider) tryReadUnsealedPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (io.ReadCloser, context.CancelFunc, error) {
	// acquire a lock purely for reading unsealed sectors
	ctx, cancel := context.WithCancel(ctx)
	if err := p.index.StorageLock(ctx, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		cancel()
		return nil, nil, xerrors.Errorf("acquiring read sector lock: %w", err)
	}

	// Reader returns a reader for an unsealed piece at the given offset in the given sector.
	// The returned reader will be nil if none of the workers has an unsealed sector file containing
	// the unsealed piece.
	r, err := p.storage.Reader(ctx, sector, abi.PaddedPieceSize(offset.Padded()), size.Padded())
	if err != nil {
		log.Debugf("did not get storage reader;sector=%+v, err:%s", sector.ID, err)
		cancel()
		return nil, nil, err
	}
	if r == nil {
		cancel()
	}

	return r, cancel, nil
}

// ReadPiece is used to read an Unsealed piece at the given offset and of the given size from a Sector
// If an Unsealed sector file exists with the Piece Unsealed in it, we'll use that for the read.
// Otherwise, we will Unseal a Sealed sector file for the given sector and read the Unsealed piece from it.
// If we do NOT have an existing unsealed file  containing the given piece thus causing us to schedule an Unseal,
// the returned boolean parameter will be set to true.
// If we have an existing unsealed file containing the given piece, the returned boolean will be set to false.
func (p *pieceProvider) ReadPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (io.ReadCloser, bool, error) {
	//return p.uns.ReadPiece(ctx, sector, offset, size, ticket, unsealed)
	log.Info("Try ReadPiece Start")
	info, err := p.uns.ReadPieceStorageInfo(ctx, sector)
	if err != nil {
		log.Error(err)
		return nil, false, err
	} else {
		log.Infof("%+v", info)
	}
	log.Info("Try ReadPiece End")
	if err := offset.Valid(); err != nil {
		return nil, false, xerrors.Errorf("offset is not valid: %w", err)
	}
	if err := size.Validate(); err != nil {
		return nil, false, xerrors.Errorf("size is not a valid piece size: %w", err)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, false, err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)

	log.Info("Start OpenUnsealedPartialFileV2")
	pf, err := partialfile.OpenUnsealedPartialFileV2(maxPieceSize, sector, info)
	if err != nil {
		if xerrors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, xerrors.Errorf("opening partial file: %w", err)
	}
	log.Info("Over OpenUnsealedPartialFileV2")
	ok, err := pf.HasAllocated(offset, size)
	if err != nil {
		_ = pf.Close()
		return nil, false, err
	}

	if !ok {
		_ = pf.Close()
		return nil, false, nil
	}

	f, err := pf.Reader(offset.Padded(), size.Padded())
	if err != nil {
		_ = pf.Close()
		return nil, false, xerrors.Errorf("getting partial file reader: %w", err)
	}

	upr, err := fr32.NewUnpadReader(f, size.Padded())
	if err != nil {
		return nil, false, xerrors.Errorf("creating unpadded reader: %w", err)
	}
	var uns bool

	log.Debugf("returning reader to read unsealed piece, sector=%+v, offset=%d, size=%d", sector, offset, size)

	return &funcCloser{
		Reader: bufio.NewReaderSize(upr, 127),
		close: func() error {
			return nil
		},
	}, uns, nil
}

type funcCloser struct {
	io.Reader
	close func() error
}

func (fc *funcCloser) Close() error { return fc.close() }
