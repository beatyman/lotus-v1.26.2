package storage

import (
	"context"
	"github.com/filecoin-project/go-address"
	"io"

	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/storage/sealing"
)

// TODO: refactor this to be direct somehow

func (m *Miner) Address() address.Address {
	return m.sealing.Address()
}

func (m *Miner) AllocatePiece(size abi.UnpaddedPieceSize) (sectorID abi.SectorNumber, offset uint64, err error) {
	return m.sealing.AllocatePiece(size)
}

func (m *Miner) SealPiece(ctx context.Context, size abi.UnpaddedPieceSize, r io.Reader, sectorID abi.SectorNumber, dealID abi.DealID) error {
	return m.sealing.SealPiece(ctx, size, r, sectorID, dealID)
}

func (m *Miner) ListSectors() ([]sealing.SectorInfo, error) {
	return m.sealing.ListSectors()
}

func (m *Miner) GetSectorInfo(sid abi.SectorNumber) (sealing.SectorInfo, error) {
	return m.sealing.GetSectorInfo(sid)
}

func (m *Miner) PledgeSector() error {
	return m.sealing.PledgeSector()
}

func (m *Miner) ForceSectorState(ctx context.Context, id abi.SectorNumber, state api.SectorState) error {
	return m.sealing.ForceSectorState(ctx, id, state)
}

// implements by hlm start
func (m *Miner) RunPledgeSector() error {
	return m.sealing.RunPledgeSector()
}
func (m *Miner) StatusPledgeSector() (int, error) {
	return m.sealing.StatusPledgeSector()
}
func (m *Miner) ExitPledgeSector() error {
	return m.sealing.ExitPledgeSector()
}

// implements by hlm end
