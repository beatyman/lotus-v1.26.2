package ffiwrapper

import (
	"context"
	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/lib/result"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime/proof"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"
	"sort"
	"sync"
	"time"
)

var errPathNotFound = xerrors.Errorf("fsstat: path not found")

func (sb *Sealer) generateHLMWindowPoSt(ctx context.Context, minerID abi.ActorID, ppt abi.RegisteredPoStProof, sectorInfo []storiface.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, []abi.SectorID, error) {
	var retErr error = nil
	randomness[31] &= 0x3f

	out := make([]proof.PoStProof, 0)

	if len(sectorInfo) == 0 {
		return nil, nil, xerrors.New("generate window post len(sectorInfo)=0")
	}

	spt := sectorInfo[0].ProofType

	maxPartitionSize, err := builtin.PoStProofWindowPoStPartitionSectors(ppt) // todo proxy through chain/actors
	if err != nil {
		return nil, nil, xerrors.Errorf("get sectors count of partition failed:%+v", err)
	}

	// We're supplied the list of sectors that the miner actor expects - this
	//  list contains substitutes for skipped sectors - but we don't care about
	//  those for the purpose of the proof, so for things to work, we need to
	//  dedupe here.
	sectorInfo = dedupeSectorInfo(sectorInfo)

	// The partitions number of this batch
	// ceil(sectorInfos / maxPartitionSize)
	partitionCount := uint64((len(sectorInfo) + int(maxPartitionSize) - 1) / int(maxPartitionSize))

	log.Infof("generateWindowPoSt maxPartitionSize:%d partitionCount:%d", maxPartitionSize, partitionCount)

	var skipped []abi.SectorID
	var flk sync.Mutex
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sort.Slice(sectorInfo, func(i, j int) bool {
		return sectorInfo[i].ID.Number < sectorInfo[j].ID.Number
	})

	sectorNums := make([]abi.SectorNumber, len(sectorInfo))
	sectorMap := make(map[abi.SectorNumber]storiface.ProofSectorInfo)
	for i, s := range sectorInfo {
		sectorNums[i] = s.ID.Number
		sectorMap[s.ID.Number] = s
	}

	postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, minerID, randomness, sectorNums)
	if err != nil {
		return nil, nil, xerrors.Errorf("generating fallback challenges: %v", err)
	}

	proofList := make([]ffi.PartitionProof, partitionCount)
	var wg sync.WaitGroup
	wg.Add(int(partitionCount))

	for partIdx := uint64(0); partIdx < partitionCount; partIdx++ {
		go func(partIdx uint64) {
			defer wg.Done()

			sectors := make([]storiface.PostSectorChallenge, 0)
			for i := uint64(0); i < maxPartitionSize; i++ {
				si := i + partIdx*maxPartitionSize
				if si >= uint64(len(postChallenges.Sectors)) {
					break
				}

				snum := postChallenges.Sectors[si]
				sinfo := sectorMap[snum]

				sectors = append(sectors, storiface.PostSectorChallenge{
					SealProof:    sinfo.ProofType,
					SectorNumber: snum,
					SealedCID:    sinfo.SealedCID,
					Challenge:    postChallenges.Challenges[snum],
					Update:       sinfo.SectorKey != nil,
				})
			}

			p, sk, err := sb.generatePartitionWindowPost(cctx, spt, ppt, minerID, int(partIdx), sectors, randomness, sectorMap)
			if err != nil || len(sk) > 0 {
				log.Errorf("generateWindowPost part:%d, skipped:%d, sectors: %d, err: %+v", partIdx, len(sk), len(sectors), err)
				flk.Lock()
				skipped = append(skipped, sk...)

				if err != nil {
					retErr = multierr.Append(retErr, xerrors.Errorf("partitionIndex:%d err:%+v", partIdx, err))
				}
				flk.Unlock()
			}

			proofList[partIdx] = ffi.PartitionProof(p)
		}(partIdx)
	}

	wg.Wait()

	if len(skipped) > 0 {
		return nil, skipped, multierr.Append(xerrors.Errorf("some sectors (%d) were skipped", len(skipped)), retErr)
	}

	postProofs, err := ffi.MergeWindowPoStPartitionProofs(ppt, proofList)
	if err != nil {
		return nil, skipped, xerrors.Errorf("merge windowPoSt partition proofs: %v", err)
	}

	out = append(out, *postProofs)
	return out, skipped, retErr
}
func (sb *Sealer) generatePartitionWindowPost(ctx context.Context, spt abi.RegisteredSealProof, ppt abi.RegisteredPoStProof, minerID abi.ActorID, partIndex int, sc []storiface.PostSectorChallenge, randomness abi.PoStRandomness, scMap map[abi.SectorNumber]storiface.ProofSectorInfo) (proof.PoStProof, []abi.SectorID, error) {
	log.Infow("generateWindowPost", "index", partIndex)

	start := time.Now()
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out, err := sb.execGenerateWindowPoSt(cctx, ppt, minerID, sc, partIndex, randomness, scMap)
	if err != nil {
		log.Error(err)
	}

	log.Warnw("generateWindowPost done", "index", partIndex, "skipped", len(out.Skipped), "took", time.Since(start).String(), "err", err)

	return out.PoStProofs, out.Skipped, err
}
func (sb *Sealer) execGenerateWindowPoSt(ctx context.Context, ppt abi.RegisteredPoStProof, mid abi.ActorID, sectors []storiface.PostSectorChallenge, partitionIdx int, randomness abi.PoStRandomness, scMap map[abi.SectorNumber]storiface.ProofSectorInfo) (storiface.WindowPoStResult, error) {

	var slk sync.Mutex
	var skipped []abi.SectorID

	var wg sync.WaitGroup
	wg.Add(len(sectors))

	var challengeThrottle = make(chan struct{}, 30) //并发数128
	var challengeReadTimeout = time.Second * 120    //单扇区超时时间
	vproofs := make([][]byte, len(sectors))

	for i, s := range sectors {
		if challengeThrottle != nil {
			select {
			case challengeThrottle <- struct{}{}:
			case <-ctx.Done():
				return storiface.WindowPoStResult{}, xerrors.Errorf("context error waiting on challengeThrottle %w", ctx.Err())
			}
		}

		go func(i int, s storiface.PostSectorChallenge) {
			defer wg.Done()
			defer func() {
				if challengeThrottle != nil {
					<-challengeThrottle
				}
			}()

			cctx, cancel := context.WithTimeout(ctx, challengeReadTimeout)
			defer cancel()
			vanilla, err := sb.generateSingleVanillaProof(cctx, mid, s, ppt, scMap[s.SectorNumber])
			slk.Lock()
			defer slk.Unlock()

			if err != nil || vanilla == nil {
				skipped = append(skipped, abi.SectorID{
					Miner:  mid,
					Number: s.SectorNumber,
				})
				log.Errorf("reading PoSt challenge for sector %d, vlen:%d, err: %s", s.SectorNumber, len(vanilla), err)
				return
			}

			vproofs[i] = vanilla
		}(i, s)
	}
	wg.Wait()

	if len(skipped) > 0 {
		// This should happen rarely because before entering GenerateWindowPoSt we check all sectors by reading challenges.
		// When it does happen, window post runner logic will just re-check sectors, and retry with newly-discovered-bad sectors skipped
		log.Errorf("couldn't read some challenges (skipped %d)", len(skipped))

		// note: can't return an error as this in an jsonrpc call
		return storiface.WindowPoStResult{Skipped: skipped}, nil
	}

	res, err := sb.GenerateWindowPoStWithVanilla(ctx, ppt, mid, randomness, vproofs, partitionIdx)
	r := storiface.WindowPoStResult{
		PoStProofs: res,
		Skipped:    skipped,
	}
	if err != nil {
		log.Errorw("generating window PoSt failed", "error", err)
		return r, xerrors.Errorf("generate window PoSt with vanilla proofs: %w", err)
	}
	return r, nil
}
func (sb *Sealer) generateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof, sector storiface.ProofSectorInfo) ([]byte, error) {
	src := storiface.SectorPaths{
		ID:          sector.SectorID(),
		Unsealed:    sector.UnsealedFile(),
		Sealed:      sector.SealedFile(),
		Cache:       sector.CachePath(),
		Update:      sector.UpdateFile(),
		UpdateCache: sector.UpdateCachePath(),
	}
	var cache, sealed string

	if si.Update {
		cache = src.UpdateCache
		sealed = src.Update
	} else {
		cache = src.Cache
		sealed = src.Sealed
	}

	if sealed == "" || cache == "" {
		return nil, errPathNotFound
	}

	psi := ffi.PrivateSectorInfo{
		SectorInfo: proof.SectorInfo{
			SealProof:    si.SealProof,
			SectorNumber: si.SectorNumber,
			SealedCID:    si.SealedCID,
		},
		CacheDirPath:     cache,
		PoStProofType:    ppt,
		SealedSectorPath: sealed,
	}

	start := time.Now()

	resCh := make(chan result.Result[[]byte], 1)
	go func() {
		resCh <- result.Wrap(ffi.GenerateSingleVanillaProof(psi, si.Challenge))
	}()

	select {
	case r := <-resCh:
		return r.Unwrap()
	case <-ctx.Done():
		log.Errorw("failed to generate valilla PoSt proof before context cancellation", "err", ctx.Err(), "duration", time.Now().Sub(start), "cache", cache, "sealed", sealed)

		// this will leave the GenerateSingleVanillaProof goroutine hanging, but that's still less bad than failing PoSt
		return nil, xerrors.Errorf("failed to generate vanilla proof before context cancellation: %w", ctx.Err())
	}
}

func dedupeSectorInfo(sectorInfo []storiface.ProofSectorInfo) []storiface.ProofSectorInfo {
	out := make([]storiface.ProofSectorInfo, 0, len(sectorInfo))
	seen := map[abi.SectorNumber]struct{}{}
	for _, info := range sectorInfo {
		if _, seen := seen[info.ID.Number]; seen {
			continue
		}
		seen[info.ID.Number] = struct{}{}
		out = append(out, info)
	}
	return out
}
