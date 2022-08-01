package storage

import (
	"context"
	"github.com/filecoin-project/lotus/buried/utils"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/pipeline/sealiface"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/api"
	pipeline "github.com/filecoin-project/lotus/storage/pipeline"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/filecoin-project/lotus/storage/sectorblocks"
)

// TODO: refactor this to be direct somehow

func (m *Miner) Address() address.Address {
	return m.sealing.Address()
}

func (m *Miner) StartPackingSector(sectorNum abi.SectorNumber) error {
	return m.sealing.StartPacking(sectorNum)
}

func (m *Miner) ListSectors() ([]pipeline.SectorInfo, error) {
	return m.sealing.ListSectors()
}

func (m *Miner) PledgeSector(ctx context.Context) (storiface.SectorRef, error) {
	return m.sealing.PledgeSector(ctx)
}

func (m *Miner) ForceSectorState(ctx context.Context, id abi.SectorNumber, state pipeline.SectorState) error {
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

func (m *Miner) SectorMatchPendingPiecesToOpenSectors(ctx context.Context) error {
	return m.sealing.MatchPendingPiecesToOpenSectors(ctx)
}

func (m *Miner) MarkForUpgrade(ctx context.Context, id abi.SectorNumber, snap bool) error {
	if snap {
		return m.sealing.MarkForSnapUpgrade(ctx, id)
	}
	return xerrors.Errorf("Old CC upgrade deprecated, use snap deals CC upgrade")
}

func (m *Miner) SectorAbortUpgrade(sectorNum abi.SectorNumber) error {
	return m.sealing.AbortUpgrade(sectorNum)
}

func (m *Miner) SectorAddPieceToAny(ctx context.Context, size abi.UnpaddedPieceSize, r storiface.Data, d api.PieceDealInfo) (api.SectorOffset, error) {
	return m.sealing.SectorAddPieceToAny(ctx, size, r, d)
}

func (m *Miner) SectorsStatus(ctx context.Context, sid abi.SectorNumber, showOnChainInfo bool) (api.SectorInfo, error) {
	if showOnChainInfo {
		return api.SectorInfo{}, xerrors.Errorf("on-chain info not supported")
	}

	info, err := m.sealing.GetSectorInfo(sid)
	if err != nil {
		return api.SectorInfo{}, err
	}

	deals := make([]abi.DealID, len(info.Pieces))
	pieces := make([]api.SectorPiece, len(info.Pieces))
	for i, piece := range info.Pieces {
		pieces[i].Piece = piece.Piece
		if piece.DealInfo == nil {
			continue
		}

		pdi := *piece.DealInfo // copy
		pieces[i].DealInfo = &pdi

		deals[i] = piece.DealInfo.DealID
	}

	log := make([]api.SectorLog, len(info.Log))
	for i, l := range info.Log {
		log[i] = api.SectorLog{
			Kind:      l.Kind,
			Timestamp: l.Timestamp,
			Trace:     l.Trace,
			Message:   l.Message,
		}
	}

	sInfo := api.SectorInfo{
		SectorID: sid,
		State:    api.SectorState(info.State),
		CommD:    info.CommD,
		CommR:    info.CommR,
		Proof:    info.Proof,
		Deals:    deals,
		Pieces:   pieces,
		Ticket: api.SealTicket{
			Value: info.TicketValue,
			Epoch: info.TicketEpoch,
		},
		Seed: api.SealSeed{
			Value: info.SeedValue,
			Epoch: info.SeedEpoch,
		},
		PreCommitMsg:         info.PreCommitMessage,
		CommitMsg:            info.CommitMessage,
		Retries:              info.InvalidProofs,
		ToUpgrade:            false,
		ReplicaUpdateMessage: info.ReplicaUpdateMessage,

		LastErr: info.LastErr,
		Log:     log,
		// on chain info
		SealProof:          info.SectorType,
		Activation:         0,
		Expiration:         0,
		DealWeight:         big.Zero(),
		VerifiedDealWeight: big.Zero(),
		InitialPledge:      big.Zero(),
		OnTime:             0,
		Early:              0,
	}

	return sInfo, nil
}

var _ sectorblocks.SectorBuilder = &Miner{}

// implements by hlm start
var PartitionsPerMsg int = 1
var EnableSeparatePartition bool = false

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
	//获取程序目录
	var currentDir, parentDir, path string
	currentDir = utils.GetCurrentDirectory()
	parentDir = utils.GetParentDirectory(currentDir)
	path = utils.GetParentDirectory(parentDir)

	id, err := storiface.ParseSectorID(sid)
	errStr := ""
	faultPath := path + "/var/log/sector_rebuild_fault.log"
	if err != nil {
		errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为: storage.ParseSectorID(sid)，失败信息为：" + err.Error()
		utils.Tracefile(errStr, faultPath)
		return errors.As(err)
	}

	sectorInfo, err := sm.sealing.GetSectorInfo(id.Number)
	if err != nil {
		return errors.As(err)
	}

	minerInfo, err := sm.api.StateMinerInfo(ctx, sm.sealing.Address(), types.EmptyTSK)
	if err != nil {
		errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为: sm.api.StateMinerInfo(ctx, sm.sealing.Address(), types.EmptyTSK)，失败信息为：" + err.Error()
		utils.Tracefile(errStr, faultPath)
		database.RebuildSectorDone(sid)
		return errors.As(err)
	}

	sealer := sm.sealer.(*sealer.Manager).Prover.(*ffiwrapper.Sealer)

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

		rspco, err := execSealPreCommit1(0, ctx, sealer, sector, sectorInfo.TicketValue, pieceInfo, faultPath)
		if err != nil {
			errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为：execSealPreCommit1(0,ctx, sealer, sector, sectorInfo.TicketValue, pieceInfo)，失败信息为：" + err.Error()
			utils.Tracefile(errStr, faultPath)
			database.RebuildSectorDone(sid)
			return errors.As(err, errStr)
		}

		p2Out, err := execSealPreCommit2(0, ctx, sealer, sector, rspco, faultPath)
		if err != nil {
			errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为：sealer.SealPreCommit2(ctx, sector, rspco)，失败信息为：" + err.Error()
			utils.Tracefile(errStr, faultPath)
			database.RebuildSectorDone(sid)
			return errors.As(err, errStr)
		}
		log.Info("p2Out===================", p2Out.Unsealed.String(), "=================sectorStatus===========", sectorInfo.CommD.String())
		if p2Out.Unsealed.String() != sectorInfo.CommD.String() {
			errStrP2 := "sector_fault_===================sector exec repair==========================" + id.Number.String() + "============" +
				p2Out.Unsealed.String() + "===" + sectorInfo.CommD.String()
			sector.SectorRepairStatus = 2
			//p2Out2, err := sealer.SealPreCommit2(ctx, sector, rspco)
			p2Out2, err := execSealPreCommit2(0, ctx, sealer, sector, rspco, faultPath)
			if err != nil {
				errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】执行第二次P2失败，方法为：sealer.SealPreCommit2(ctx, sector, rspco)，失败信息为：" + err.Error()
				utils.Tracefile(errStr, faultPath)
				database.RebuildSectorDone(sid)
				return errors.As(err, errStr)
			}
			if p2Out2.Unsealed.String() != sectorInfo.CommD.String() {
				errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，失败信息为： 两次重算P2得到的CommD 不一致，" + errStrP2 + "===" + p2Out2.Unsealed.String() + "====" + sectorInfo.CommD.String()
				utils.Tracefile(errStr, faultPath)
				database.RebuildSectorDone(sid)
				return errors.New("sector_fault_=====================Sector cannot be recovered=====================")
			}

		}

		// update storage
		if err := database.SetSectorSealedStorage(sid, storageId); err != nil {
			errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为：database.SetSectorSealedStorage(sid, storageId)，失败信息为：" + err.Error()
			utils.Tracefile(errStr, faultPath)
			return errors.As(err)
		}
		// do finalize
		if err := sealer.FinalizeSector(ctx, sector, nil); err != nil {
			errStr = "sector_fault_扇区编号为：【" + id.Number.String() + "】失败，方法为：sealer.FinalizeSector(ctx, sector, nil)，失败信息为：" + err.Error()
			utils.Tracefile(errStr, faultPath)
			return errors.As(err)
		}
		if err := database.RebuildSectorDone(sid); err != nil {
			return errors.As(err)
		}
		successMsg := "扇区" + id.Number.String() + "恢复成功"
		utils.Tracefile(successMsg, path+"/var/log/sector_rebuild_success.log")
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

func execSealPreCommit1(retryCount int64, ctx context.Context, sealer *ffiwrapper.Sealer,
	sector storage.SectorRef, ticketValue abi.SealRandomness, pieceInfo []abi.PieceInfo, faultPath string) (storage.PreCommit1Out, error) {
	rspco, err := sealer.SealPreCommit1(ctx, sector, ticketValue, pieceInfo)
	retryCount++
	if retryCount > 3 {
		errStr := ""
		if err != nil {
			errStr = "sector_fault_扇区编号为：【" + sector.ID.Number.String() + "】失败，方法为：sealer.SealPreCommit1(ctx, sector, ticketValue, pieceInfo)，失败信息为：" + err.Error()
			log.Error("=================================================", err)
		}
		utils.Tracefile(errStr, faultPath)
		return rspco, errors.New("error retryCount 3")
	}
	if err != nil {
		return execSealPreCommit1(retryCount, ctx, sealer, sector, ticketValue, pieceInfo, faultPath)
	}
	return rspco, nil
}

func execSealPreCommit2(retryCount int64, ctx context.Context, sealer *ffiwrapper.Sealer,
	sector storage.SectorRef, rspco storage.PreCommit1Out, faultPath string) (storage.SectorCids, error) {
	p2Out, err := sealer.SealPreCommit2(ctx, sector, rspco)
	retryCount++
	if retryCount > 3 {
		errStr := ""
		if err != nil {
			errStr = "sector_fault_扇区编号为：【" + sector.ID.Number.String() + "】失败，方法为：sealer.SealPreCommit1(ctx, sector, ticketValue, pieceInfo)，失败信息为：" + err.Error()
			log.Error("=================================================", err)
		}
		utils.Tracefile(errStr, faultPath)
		return p2Out, errors.New("error retryCount 3")
	}
	if err != nil {
		errStr := "sector_fault_扇区编号为：【" + sector.ID.Number.String() + "】失败，方法为：sealer.SealPreCommit2(ctx, sector, rspco)，失败信息为：" + err.Error()
		utils.Tracefile(errStr, faultPath)
		return execSealPreCommit2(retryCount, ctx, sealer, sector, rspco, faultPath)
	}
	return p2Out, nil
}

// implements by hlm end
