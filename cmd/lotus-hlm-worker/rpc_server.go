package main

import (
	"context"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/gwaylib/errors"
)

type rpcServer struct {
	sb *ffiwrapper.Sealer
}

func (w *rpcServer) Version(context.Context) (build.Version, error) {
	return build.APIVersion, nil
}

func (w *rpcServer) SealCommit2(ctx context.Context, sector abi.SectorID, commit1Out storage.Commit1Out) (storage.Proof, error) {
	log.Infof("SealCommit2 RPC in:%d", sector)
	defer log.Infof("SealCommit2 RPC out:%d", sector)

	return w.sb.SealCommit2(ctx, sector, commit1Out)
}

func (w *rpcServer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []abi.SectorInfo, randomness abi.PoStRandomness) ([]abi.PoStProof, error) {
	log.Infof("GenerateWinningPoSt RPC in:%d", minerID)
	defer log.Infof("GenerateWinningPoSt RPC out:%d", minerID)

	return w.sb.GenerateWinningPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *rpcServer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []abi.SectorInfo, randomness abi.PoStRandomness) (api.WindowPoStResp, error) {
	log.Infof("GenerateWindowPoSt RPC in:%d", minerID)
	defer log.Infof("GenerateWindowPoSt RPC out:%d", minerID)

	proofs, ignore, err := w.sb.GenerateWindowPoSt(ctx, minerID, sectorInfo, randomness)
	if err != nil {
		return api.WindowPoStResp{}, errors.As(err)
	}
	return api.WindowPoStResp{
		Proofs: proofs,
		Ignore: ignore,
	}, nil
}
