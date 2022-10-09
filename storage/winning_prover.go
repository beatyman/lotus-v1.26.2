package storage

import (
	"context"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/gwaylib/errors"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

var log = logging.Logger("storageminer")

type StorageWpp struct {
	prover   storiface.ProverPoSt
	verifier storiface.Verifier
	miner    abi.ActorID
	winnRpt  abi.RegisteredPoStProof
}

func NewWinningPoStProver(api v1api.FullNode, prover storiface.ProverPoSt, verifier storiface.Verifier, miner dtypes.MinerID) (*StorageWpp, error) {
	ma, err := address.NewIDAddress(uint64(miner))
	if err != nil {
		return nil, err
	}

	mi, err := api.StateMinerInfo(context.TODO(), ma, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	if build.InsecurePoStValidation {
		log.Warn("*****************************************************************************")
		log.Warn(" Generating fake PoSt proof! You should only see this while running tests! ")
		log.Warn("*****************************************************************************")
	}

	return &StorageWpp{prover, verifier, abi.ActorID(miner), mi.WindowPoStProofType}, nil
}

var _ gen.WinningPoStProver = (*StorageWpp)(nil)

func (wpp *StorageWpp) GenerateCandidates(ctx context.Context, randomness abi.PoStRandomness, eligibleSectorCount uint64) ([]uint64, error) {
	start := build.Clock.Now()

	cds, err := wpp.verifier.GenerateWinningPoStSectorChallenge(ctx, wpp.winnRpt, wpp.miner, randomness, eligibleSectorCount)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate candidates: %w", err)
	}
	log.Infof("Generate candidates took %s (C: %+v)", time.Since(start), cds)
	return cds, nil
}

func (wpp *StorageWpp) ComputeProof(ctx context.Context, ssi []builtin.ExtendedSectorInfo, rand abi.PoStRandomness, currEpoch abi.ChainEpoch, nv network.Version) ([]builtin.PoStProof, error) {
	if build.InsecurePoStValidation {
		return []builtin.PoStProof{{ProofBytes: []byte("valid proof")}}, nil
	}
	repo := ""
	sm, ok := wpp.prover.(*sealer.Manager)
	if ok {
		sb, ok := sm.Prover.(*ffiwrapper.Sealer)
		if ok {
			repo = sb.RepoPath()
		}
	}
	if len(repo) == 0 {
		log.Warn("not found default repo")
	}
	rSectors := []storiface.SectorRef{}
	pSectors := []storiface.ProofSectorInfo{}
	for _, s := range ssi {
		id := abi.SectorID{Miner: wpp.miner, Number: s.SectorNumber}
		sFile, err := database.GetSectorFile(storiface.SectorName(id), repo)
		if err != nil {
			return nil, err
		}
		rSector := storiface.SectorRef{
			ID:         id,
			ProofType:  s.SealProof,
			SectorFile: *sFile,
		}
		rSectors = append(rSectors, rSector)
		pSectors = append(pSectors, storiface.ProofSectorInfo{
			SectorRef: rSector,
			SealedCID: s.SealedCID,
		})
	}
	// check files
	_, _, bad, err := ffiwrapper.CheckProvable(ctx,wpp.winnRpt ,rSectors, nil, 6*time.Second)
	if err != nil {
		return nil, errors.As(err)
	}
	if len(bad) > 0 {
		return nil, xerrors.Errorf("pubSectorToPriv skipped sectors: %+v", bad)
	}
	log.Infof("Computing WinningPoSt ;%+v; %v", ssi, rand)

	start := build.Clock.Now()
	proof, err := wpp.prover.GenerateWinningPoSt(ctx, wpp.miner, pSectors, rand)
	if err != nil {
		return nil, err
	}
	log.Infof("GenerateWinningPoSt took %s", time.Since(start))
	return proof, nil
}
