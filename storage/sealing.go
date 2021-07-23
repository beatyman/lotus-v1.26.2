package storage

import (
	"context"
	"io"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-storage/storage"

	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	sealing "github.com/filecoin-project/lotus/extern/storage-sealing"
	"github.com/filecoin-project/lotus/extern/storage-sealing/sealiface"
)

// TODO: refactor this to be direct somehow

func (m *Miner) Address() address.Address {
	return m.sealing.Address()
}

func (m *Miner) AddPieceToAnySector(ctx context.Context, size abi.UnpaddedPieceSize, r io.Reader, d sealing.DealInfo) (abi.SectorNumber, abi.PaddedPieceSize, error) {
	return m.sealing.AddPieceToAnySector(ctx, size, r, d)
}

func (m *Miner) StartPackingSector(sectorNum abi.SectorNumber) error {
	return m.sealing.StartPacking(sectorNum)
}

func (m *Miner) ListSectors() ([]sealing.SectorInfo, error) {
	return m.sealing.ListSectors()
}

func (m *Miner) GetSectorInfo(sid abi.SectorNumber) (sealing.SectorInfo, error) {
	return m.sealing.GetSectorInfo(sid)
}

func (m *Miner) PledgeSector(ctx context.Context) (storage.SectorRef, error) {
	return m.sealing.PledgeSector(ctx)
}

func (m *Miner) ForceSectorState(ctx context.Context, id abi.SectorNumber, state sealing.SectorState) error {
	return m.sealing.ForceSectorState(ctx, id, state)
}

func (m *Miner) RemoveSector(ctx context.Context, id abi.SectorNumber) error {
	return m.sealing.Remove(ctx, id)
}

func (m *Miner) TerminateSector(ctx context.Context, id abi.SectorNumber) error {
	return m.sealing.Terminate(ctx, id)
}

func (m *Miner) TerminateFlush(ctx context.Context) (*cid.Cid, error) {
	return m.sealing.TerminateFlush(ctx)
}

func (m *Miner) TerminatePending(ctx context.Context) ([]abi.SectorID, error) {
	return m.sealing.TerminatePending(ctx)
}

func (m *Miner) SectorPreCommitFlush(ctx context.Context) ([]sealiface.PreCommitBatchRes, error) {
	return m.sealing.SectorPreCommitFlush(ctx)
}

func (m *Miner) SectorPreCommitPending(ctx context.Context) ([]abi.SectorID, error) {
	return m.sealing.SectorPreCommitPending(ctx)
}

func (m *Miner) CommitFlush(ctx context.Context) ([]sealiface.CommitBatchRes, error) {
	return m.sealing.CommitFlush(ctx)
}

func (m *Miner) CommitPending(ctx context.Context) ([]abi.SectorID, error) {
	return m.sealing.CommitPending(ctx)
}

func (m *Miner) MarkForUpgrade(id abi.SectorNumber) error {
	return m.sealing.MarkForUpgrade(id)
}
func (m *Miner) IsMarkedForUpgrade(id abi.SectorNumber) bool {
	return m.sealing.IsMarkedForUpgrade(id)
}

// implements by hlm start

func (m *Miner) WdpostEnablePartitionSeparate(enable bool) error {
	log.Info("lookup enable:", enable)
	EnableSeparatePartition = enable
	log.Info("lookup EnableSeparatePartition:", EnableSeparatePartition)
	return nil
}
func (m *Miner) WdpostSetPartitionNumber(number int) error {
	log.Info("lookup number:", number)
	PartitionsPerMsg = number
	log.Info("lookup PartitionsPerMsg:", PartitionsPerMsg)
	return nil
}

func (m *Miner) RunPledgeSector() error {
	return m.sealing.RunPledgeSector()
}
func (m *Miner) StatusPledgeSector() (int, error) {
	return m.sealing.StatusPledgeSector()
}
func (m *Miner) ExitPledgeSector() error {
	return m.sealing.ExitPledgeSector()
}
func (sm *Miner) RebuildSector(ctx context.Context, sid string, storageId uint64) error {
	id, err := storage.ParseSectorID(sid)
	if err != nil {
		return errors.As(err)
	}

	sectorInfo, err := sm.sealing.GetSectorInfo(id.Number)
	if err != nil {
		return errors.As(err)
	}
	minerInfo, err := sm.api.StateMinerInfo(ctx, sm.sealing.Address(), types.EmptyTSK)
	if err != nil {
		return errors.As(err)
	}

	sealer := sm.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)

	// reset state
	if _, err := sealer.UpdateSectorState(sid, "rebuild sector", 200, true, true); err != nil {
		return errors.As(err)
	}

	rebuild := func() error {
		if err := database.RebuildSector(sid, storageId); err != nil {
			return errors.As(err)
		}
		sector := storage.SectorRef{
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
		if err := sealer.FinalizeSector(ctx, sector, nil); err != nil {
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

// implements by hlm end