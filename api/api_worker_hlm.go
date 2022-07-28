package api

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
)

type WindowPoStResp struct {
	Proofs []proof.PoStProof
	Ignore []abi.SectorID
}

// 兼容老c2
type SectorRef struct {
	abi.SectorID
	ProofType abi.RegisteredSealProof
	TaskKey   string //用于UnlockGPUService
}

type WorkerHlmAPI interface {
	Version(context.Context) (string, error)

	SealCommit2(context.Context, SectorRef, storiface.Commit1Out) (storiface.Proof, error)
	GenerateWinningPoSt(context.Context, abi.ActorID, []storiface.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error)
	GenerateWindowPoSt(context.Context, abi.ActorID, []storiface.ProofSectorInfo, abi.PoStRandomness) (WindowPoStResp, error)
}
