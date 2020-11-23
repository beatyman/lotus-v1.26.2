package apistruct

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
)

type WorkerHlmStruct struct {
	Internal struct {
		Version func(context.Context) (build.Version, error) `perm:"read"`

		SealCommit2         func(context.Context, storage.SectorRef, storage.Commit1Out) (storage.Proof, error)                           `perm:"admin"`
		GenerateWinningPoSt func(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error)  `perm:"admin"`
		GenerateWindowPoSt  func(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) (api.WindowPoStResp, error) `perm:"admin"`
	}
}

func (w *WorkerHlmStruct) Version(ctx context.Context) (build.Version, error) {
	return w.Internal.Version(ctx)
}

func (w *WorkerHlmStruct) SealCommit2(ctx context.Context, sector storage.SectorRef, commit1Out storage.Commit1Out) (storage.Proof, error) {
	return w.Internal.SealCommit2(ctx, sector, commit1Out)
}

func (w *WorkerHlmStruct) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	return w.Internal.GenerateWinningPoSt(ctx, minerID, sectorInfo, randomness)
}
func (w *WorkerHlmStruct) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) (api.WindowPoStResp, error) {
	return w.Internal.GenerateWindowPoSt(ctx, minerID, sectorInfo, randomness)
}

var _ api.WorkerHlmAPI = &WorkerHlmStruct{}
