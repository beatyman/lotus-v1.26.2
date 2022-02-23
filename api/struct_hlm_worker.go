package api

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
)

type WorkerHlmStruct struct {
	Internal struct {
		Version func(context.Context) (string, error) `perm:"read"`

		SealCommit2         func(context.Context, SectorRef, storage.Commit1Out) (storage.Proof, error)                                  `perm:"admin"`
		GenerateWinningPoSt func(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error) `perm:"admin"`
		GenerateWindowPoSt  func(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) (WindowPoStResp, error)    `perm:"admin"`
		ProveReplicaUpdate2 func(context.Context, SectorRef, storage.ReplicaVanillaProofs) (storage.ReplicaUpdateProof, error)           `perm:"admin"`
	}
}

func (w *WorkerHlmStruct) Version(ctx context.Context) (string, error) {
	return w.Internal.Version(ctx)
}

func (w *WorkerHlmStruct) SealCommit2(ctx context.Context, sector SectorRef, commit1Out storage.Commit1Out) (storage.Proof, error) {
	return w.Internal.SealCommit2(ctx, sector, commit1Out)
}

func (w *WorkerHlmStruct) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	return w.Internal.GenerateWinningPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *WorkerHlmStruct) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) (WindowPoStResp, error) {
	return w.Internal.GenerateWindowPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *WorkerHlmStruct) ProveReplicaUpdate2(ctx context.Context, sector SectorRef, vanillaProofs storage.ReplicaVanillaProofs) (storage.ReplicaUpdateProof, error) {
	return w.Internal.ProveReplicaUpdate2(ctx, sector, vanillaProofs)
}

var _ WorkerHlmAPI = &WorkerHlmStruct{}
