package sealing

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
)

func (m *Sealing) PledgeSector() error {
	cfg, err := m.getConfig()
	if err != nil {
		return xerrors.Errorf("getting config: %w", err)
	}

	if cfg.MaxSealingSectors > 0 {
		if m.stats.curSealing() >= cfg.MaxSealingSectors {
			return xerrors.Errorf("too many sectors sealing (curSealing: %d, max: %d)", m.stats.curSealing(), cfg.MaxSealingSectors)
		}
	}

	go func() {
		ctx := context.TODO() // we can't use the context from command which invokes
		// this, as we run everything here async, and it's cancelled when the
		// command exits

		spt, err := m.currentSealProof(ctx)
		if err != nil {
			log.Errorf("%+v", err)
			return
		}

		size, err := spt.SectorSize()
		if err != nil {
			log.Errorf("%+v", err)
			return
		}

		sid, err := m.sc.Next()
		if err != nil {
			log.Errorf("%+v", err)
			return
		}
		sectorID := m.minerSector(spt, sid)
		err = m.sealer.NewSector(ctx, sectorID)
		if err != nil {
			log.Errorf("%+v", err)
			return
		}

		pieces, err := m.sealer.PledgeSector(ctx, sectorID, []abi.UnpaddedPieceSize{}, abi.PaddedPieceSize(size).Unpadded())
		if err != nil {
			log.Errorf("%+v", err)
			return
		}

		ps := make([]Piece, len(pieces))
		for idx := range ps {
			ps[idx] = Piece{
				Piece:    pieces[idx],
				DealInfo: nil,
			}
		}

		if err := m.newSectorCC(ctx, sid, ps); err != nil {
			log.Errorf("%+v", err)
			return
		}
	}()
	return nil
}
