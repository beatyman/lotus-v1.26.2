package sealing

import (
	"context"

	abi "github.com/filecoin-project/go-state-types/abi"

	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/gwaylib/errors"
)

// export Sealing.PledgeSector for remote worker for calling.
func (m *Sealing) PledgeRemoteSector() error {
	// design this function, so it's easy to make a vimdiff compare.
	return func() error {
		ctx := context.TODO() // we can't use the context from command which invokes
		// this, as we run everything here async, and it's cancelled when the
		// command exits

		spt, err := m.currentSealProof(ctx)
		if err != nil {
			return errors.As(err)
		}

		size, err := spt.SectorSize()
		if err != nil {
			return errors.As(err)
		}

		sid, err := m.sc.Next()
		if err != nil {
			return errors.As(err)
		}
		sectorID := m.minerSector(spt, sid)
		if err := m.sealer.NewSector(ctx, sectorID); err != nil {
			return errors.As(err, sid)
		}

		sb := m.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)
		pieces, err := sb.PledgeSector(ctx, sectorID, []abi.UnpaddedPieceSize{abi.PaddedPieceSize(size).Unpadded()})
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
		if err := m.newSectorCC(ctx, sid, ps); err != nil {
			return errors.As(err)
		}
		return nil
	}()
}
