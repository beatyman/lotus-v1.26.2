package main

import (
	"context"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-storage/storage"
)

type rpcServer struct {
	sb *ffiwrapper.Sealer
}

func (w *rpcServer) Version(context.Context) (build.Version, error) {
	return build.APIVersion, nil
}

func (w *rpcServer) SealCommit2(ctx context.Context, sector abi.SectorID, commit1Out storage.Commit1Out) (storage.Proof, error) {
	return w.sb.SealCommit2(ctx, sector, commit1Out)
}

func (w *rpcServer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []abi.SectorInfo, randomness abi.PoStRandomness) ([]abi.PoStProof, error) {
	return w.sb.GenerateWinningPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *rpcServer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []abi.SectorInfo, randomness abi.PoStRandomness) ([]abi.PoStProof, []abi.SectorID, error) {
	return w.sb.GenerateWindowPoSt(ctx, minerID, sectorInfo, randomness)
}
