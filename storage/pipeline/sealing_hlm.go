package sealing

import (
	"context"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/go-state-types/abi"
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
	/*
		minerInfo, err := m.Api.StateMinerInfo(ctx, m.Address(), types.EmptyTSK)
		if err != nil {
			return errors.As(err)
		}
	*/
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
			ProofType: sectorInfo.SectorType,
		}
		var pieceInfo []abi.PieceInfo
		var existingPieceSizes []abi.UnpaddedPieceSize
		var allocated abi.UnpaddedPieceSize
		var fillerSizes []abi.UnpaddedPieceSize
		for _, piece := range sectorInfo.Pieces {
			if piece.DealInfo != nil {
				prop, err := piece.DealInfo.DealProposal.Cid()
				if err != nil {
					log.Error(err.Error())
					return errors.As(err)
				}
				deal, err := database.GetMarketDealInfo(prop.String())
				if err != nil {
					log.Error(err.Error())
					return errors.As(err)
				}
				pieceData := shared.PieceDataInfo{
					ReaderKind:     shared.PIECE_DATA_KIND_FILE,
					LocalPath:      deal.FileLocal,
					ServerStorage:  deal.FileStorage,
					ServerFullUri:  deal.FileRemote,
					ServerFileName: deal.FileRemote,
					UnpaddedSize:   piece.Piece.Size.Unpadded(),
				}
				out, err := sealer.AddPiece(ctx, sector, existingPieceSizes, piece.Piece.Size.Unpadded(), pieceData)
				if err != nil {
					log.Error(err.Error())
					return errors.As(err)
				}
				pieceInfo = append(pieceInfo, out)
				existingPieceSizes = append(existingPieceSizes, out.Size.Unpadded())
				allocated += piece.Piece.Size.Unpadded()
			} else {
				fillerSizes = append(fillerSizes, piece.Piece.Size.Unpadded())
			}
		}
		pieceInfoPad, err := m.padSector(ctx, sector, existingPieceSizes, fillerSizes...)
		if err != nil {
			log.Error(err.Error())
			return errors.As(err)
		}
		pieceInfo = append(pieceInfo, pieceInfoPad...)
		/*
			var pieceInfo []abi.PieceInfo
			var existingPieceSizes []abi.UnpaddedPieceSize
			for _, piece := range sectorInfo.Pieces {
				if piece.DealInfo != nil {
					prop, err := piece.DealInfo.DealProposal.Cid()
					if err != nil {
						log.Error(err.Error())
						return errors.As(err)
					}
					deal, err := database.GetMarketDealInfo(prop.String())
					if err != nil {
						log.Error(err.Error())
						return errors.As(err)
					}
					pieceData := shared.PieceDataInfo{
						ReaderKind:     shared.PIECE_DATA_KIND_FILE,
						LocalPath:      deal.FileLocal,
						ServerStorage:  deal.FileStorage,
						ServerFullUri:  deal.FileRemote,
						ServerFileName: deal.FileRemote,
					}
					out, err := sealer.AddPiece(ctx, sector, []abi.UnpaddedPieceSize{}, abi.UnpaddedPieceSize(deal.PieceSize), pieceData)
					if err != nil {
						log.Error(err.Error())
						return errors.As(err)
					}
					existingPieceSizes = append(existingPieceSizes, out.Size.Unpadded())
				}
			}
			ssize := minerInfo.SectorSize
			pieceInfo, err = sealer.PledgeSector(ctx, sector,
				existingPieceSizes,
				abi.PaddedPieceSize(ssize).Unpadded(),
			)
			if err != nil {
				log.Error(err.Error())
				return errors.As(err)
			}*/
		rspco, err := sealer.SealPreCommit1(ctx, sector, sectorInfo.TicketValue, pieceInfo)
		if err != nil {
			log.Error(err.Error())
			return errors.As(err)
		}
		_, err = sealer.SealPreCommit2(ctx, sector, rspco)
		if err != nil {
			log.Error(err.Error())
			return errors.As(err)
		}
		// update storage
		if err := database.SetSectorSealedStorage(sid, storageId); err != nil {
			log.Error(err.Error())
			return errors.As(err)
		}
		// do finalize
		if err := sealer.FinalizeSector(ctx, sector); err != nil {
			log.Error(err.Error())
			return errors.As(err)
		}
		if err := database.RebuildSectorDone(sid); err != nil {
			log.Error(err.Error())
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
