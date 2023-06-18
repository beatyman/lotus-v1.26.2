package sealing

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
)

// implements by hlm start
var PartitionsPerMsg int = 1
var EnableSeparatePartition bool = false
func (m *Sealing) WdpostEnablePartitionSeparate(enable bool) error {
	log.Info("lookup enable:", enable)
	EnableSeparatePartition = enable
	log.Info("lookup EnableSeparatePartition:", EnableSeparatePartition)
	return nil
}
func (m *Sealing) WdpostSetPartitionNumber(number int) error {
	log.Info("lookup number:", number)
	PartitionsPerMsg = number
	log.Info("lookup PartitionsPerMsg:", PartitionsPerMsg)
	return nil
}


func (m *Sealing) RebuildSector(ctx context.Context, sid string, storageId uint64) error {
	id, err := storiface.ParseSectorID(sid)
	if err != nil {
		return errors.As(err)
	}

	sectorInfo, err := m.GetSectorInfo(id.Number)
	if err != nil {
		return errors.As(err)
	}
	minerInfo, err := m.Api.StateMinerInfo(ctx, m.Address(), types.EmptyTSK)
	if err != nil {
		return errors.As(err)
	}

	sealer := m.sealer.(*sealer.Manager).Prover.(*ffiwrapper.Sealer)

	// reset state
	if _, err := sealer.UpdateSectorState(sid, "rebuild sector", 200, true, true); err != nil {
		return errors.As(err)
	}

	rebuild := func() error {
		if err := database.RebuildSector(sid, storageId); err != nil {
			return errors.As(err)
		}
		sector := storiface.SectorRef{
			ID:        id,
			ProofType: abi.RegisteredSealProof(minerInfo.WindowPoStProofType),
		}
		ssize := minerInfo.SectorSize
		pieceInfo, err := sealer.PledgeSector(ctx, sector,
			[]abi.UnpaddedPieceSize{},
			abi.PaddedPieceSize(ssize).Unpadded(),
		)
		if err != nil {
			return errors.As(err)
		}
		rspco, err := sealer.SealPreCommit1(ctx, sector, sectorInfo.TicketValue, pieceInfo)
		if err != nil {
			return errors.As(err)
		}
		_, err = sealer.SealPreCommit2(ctx, sector, rspco)
		if err != nil {
			return errors.As(err)
		}
		// update storage
		if err := database.SetSectorSealedStorage(sid, storageId); err != nil {
			return errors.As(err)
		}
		// do finalize
		if err := sealer.FinalizeSector(ctx, sector); err != nil {
			return errors.As(err)
		}
		if err := database.RebuildSectorDone(sid); err != nil {
			return errors.As(err)
		}
		return nil
	}

	go func() {
		if err := rebuild(); err != nil {
			log.Warn(errors.As(err))
		} else {
			log.Infof("Rebuild sector %s done, storage %d", sid, storageId)
		}
	}()

	return nil
}


func (m *Sealing) Maddr() string {
	log.Info("addr:", m.maddr.String())
	return string(m.maddr.String())
}

// implements by hlm end