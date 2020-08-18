package sealing

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"
	xerrors "golang.org/x/xerrors"

	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/gwaylib/errors"
)

type Pledge struct {
	SectorID abi.SectorID
	Sealing  *Sealing

	SectorBuilder *ffiwrapper.Sealer

	ActAddr address.Address

	ExistingPieceSizes []abi.UnpaddedPieceSize
	Sizes              []abi.UnpaddedPieceSize
}

// Export the garbage.go#sealing.pledgeSector, so they should have same logic.
func (g *Pledge) PledgeSector(ctx context.Context) ([]abi.PieceInfo, error) {
	sectorID := g.SectorID
	sizes := g.Sizes
	existingPieceSizes := g.ExistingPieceSizes
	log.Infof("DEBUG:PledgeSector in, %d,%d", sectorID, len(sizes))
	defer log.Infof("DEBUG:PledgeSector out, %d", sectorID)

	if len(sizes) == 0 {
		log.Infof("DEBGUG:PledgeSector no sizes, %d", sectorID)
		return nil, nil
	}

	log.Infof("Pledge %d, contains %+v", sectorID, existingPieceSizes)

	sb := g.Sealing.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)
	out := make([]abi.PieceInfo, len(sizes))
	for i, size := range sizes {
		ppi, err := sb.AddPiece(ctx, sectorID, existingPieceSizes, size, NewNullReader(size))
		if err != nil {
			return nil, xerrors.Errorf("add piece: %w", err)
		}

		g.ExistingPieceSizes = append(g.ExistingPieceSizes, size)

		out[i] = ppi
	}

	return out, nil
}

// export Sealing.PledgeSector for remote worker for calling.
func (m *Sealing) PledgeRemoteSector() error {
	// design a func so, it's easy to make a vimdiff compare.
	return func() error {
		ctx := context.TODO() // we can't use the context from command which invokes
		// this, as we run everything here async, and it's cancelled when the
		// command exits

		size := abi.PaddedPieceSize(m.sealer.SectorSize()).Unpadded()

		sid, err := m.sc.Next()
		if err != nil {
			return errors.As(err)
		}
		sectorID := m.minerSector(sid)
		if err := m.sealer.NewSector(ctx, sectorID); err != nil {
			return errors.As(err, sid)
		}

		sb := m.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)
		pieces, err := sb.PledgeSector(sectorID, []abi.UnpaddedPieceSize{size})
		if err != nil {
			return errors.As(err)
		}

		ps := make([]Piece, len(pieces))
		for idx := range ps {
			ps[idx] = Piece{
				Piece:    pieces[idx],
				DealInfo: nil,
			}
		}
		if err := m.newSectorCC(sid, ps); err != nil {
			return errors.As(err)
		}
		return nil
	}()
}
