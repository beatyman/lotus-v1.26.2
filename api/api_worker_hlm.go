package api

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/ipfs/go-cid"
)

type WindowPoStResp struct {
	Proofs []proof.PoStProof
	Ignore []abi.SectorID
}

// 兼容老c2
type SectorRef struct {
	abi.SectorID
	ProofType abi.RegisteredSealProof

	TaskKey string //用于UnlockGPUService
	//ProveUpdate
	SectorKey   cid.Cid
	NewSealed   cid.Cid
	NewUnsealed cid.Cid
}

type WorkerHlmAPI interface {
	Version(context.Context) (string, error)

	SealCommit2(context.Context, SectorRef, storage.Commit1Out) (storage.Proof, error)
	GenerateWinningPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error)
	GenerateWindowPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) (WindowPoStResp, error)
	ProveReplicaUpdate2(context.Context, SectorRef, storage.ReplicaVanillaProofs) (storage.ReplicaUpdateProof, error)
}
