package api

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
)

type WindowPoStResp struct {
	Proofs []proof.PoStProof
	Ignore []abi.SectorID
}

// 兼容老c2
type SectorRef struct {
	abi.SectorID
	ProofType abi.RegisteredSealProof
}

type WorkerHlmAPI interface {
	Version(context.Context) (build.Version, error)

	SealCommit2(context.Context, SectorRef, storage.Commit1Out) (storage.Proof, error)
	GenerateWinningPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error)
	GenerateWindowPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) (WindowPoStResp, error)
}
