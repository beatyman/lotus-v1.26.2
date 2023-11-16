package wdpost

import (
	"bytes"
	"context"
	"fmt"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"time"

	"github.com/gwaylib/errors"

	"github.com/filecoin-project/go-bitfield"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin/v9/miner"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime/proof"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"

	"github.com/filecoin-project/lotus/chain/types"
)

func (s *WindowPoStScheduler) runHlmPoStCycle(ctx context.Context, manual bool, di dline.Info, ts *types.TipSet) ([]miner.SubmitWindowedPoStParams, error) {
	log.Info("================================ DEBUG: Start generage wdpost========================================")
	defer log.Info("================================DEBUG: End generage wdpost========================================")
	epochTime := int64(build.BlockDelaySecs)
	maxDelayEpoch := int64(di.Close - di.CurrentEpoch - 10)
	maxDelayTime := maxDelayEpoch * epochTime
	log.Infof("max delay time:%d, max delay epoch:%d, epoch time:%d",
		maxDelayTime, maxDelayEpoch, epochTime)
	timech := time.After(time.Duration(maxDelayTime) * time.Second)
	log.Infof("deadline info:  index=%d, current epoch=%d, Challenge epoch=%d, PeriodStart=%d, Open epoch=%d, close epoch=%d",
		di.Index, di.CurrentEpoch, di.Challenge, di.PeriodStart, di.Open, di.Close)
	log.Infof("WPoStPeriodDeadlines:%d, WPoStProvingPeriod=%d, WPoStChallengeWindow=%d, WPoStChallengeLookback=%d FaultDeclarationCutoff=%d",
		di.WPoStPeriodDeadlines, di.WPoStPeriodDeadlines, di.WPoStChallengeWindow, di.WPoStChallengeLookback, di.FaultDeclarationCutoff,
	)

	go func() {
		// TODO: extract from runPoStCycle, run on fault cutoff boundaries
		s.asyncFaultRecover(di, ts)
	}()

	buf := new(bytes.Buffer)
	if err := s.actor.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	headTs, err := s.api.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting current head: %w", err)
	}

	rand, err := s.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), headTs.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
	}

	// Get the partitions for the given deadline
	partitions, err := s.api.StateMinerPartitions(ctx, s.actor, di.Index, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting partitions: %w", err)
	}

	nv, err := s.api.StateNetworkVersion(ctx, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting network version: %w", err)
	}

	// Split partitions into batches, so as not to exceed the number of sectors
	// allowed in a single message
	partitionBatches, err := s.BatchPartitions(partitions, nv)
	if err != nil {
		return nil, err
	}

	// Generate proofs in batches
	posts := make([]miner.SubmitWindowedPoStParams, 0, len(partitionBatches))
	postChan := make(chan interface{}, 1)

	batchPoSt := func(batchIdx int, batch []api.Partition) ([]miner.SubmitWindowedPoStParams, error) {
		batchPartitionStartIdx := 0
		for _, batch := range partitionBatches[:batchIdx] {
			batchPartitionStartIdx += len(batch)
		}

		params := miner.SubmitWindowedPoStParams{
			Deadline:   di.Index,
			Partitions: make([]miner.PoStPartition, 0, len(batch)),
			Proofs:     nil,
		}

		skipCount := uint64(0)
		postSkipped := bitfield.New()
		somethingToProve := false

		var batchPartitionIndexes []string
		for idx := 0; idx < len(batch); idx++ {
			batchPartitionIndexes = append(batchPartitionIndexes, fmt.Sprintf("%v", batchPartitionStartIdx+idx))
		}

		// Retry until we run out of sectors to prove.
		for retries := 0; ; retries++ {
			var partitions []miner.PoStPartition
			var sinfos []storiface.ProofSectorInfo
			for partIdx, partition := range batch {
				// TODO: Can do this in parallel
				toProve, err := bitfield.SubtractBitField(partition.LiveSectors, partition.FaultySectors)
				if err != nil {
					return nil, xerrors.Errorf("removing faults from set of sectors to prove: %w", err)
				}
				toProve, err = bitfield.MergeBitFields(toProve, partition.RecoveringSectors)
				if err != nil {
					return nil, xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err)
				}
				good, err := toProve.Copy()
				if err != nil {
					return nil, xerrors.Errorf("copy toProve: %w", err)
				}
				if !s.disablePreChecks {
					good, err = s.checkSectors(ctx, toProve, ts.Key(), time.Second*120)
					if err != nil {
						return nil, xerrors.Errorf("checking sectors to skip: %w", err)
					}
				}
				good, err = bitfield.SubtractBitField(good, postSkipped)
				if err != nil {
					return nil, xerrors.Errorf("toProve - postSkipped: %w", err)
				}

				skipped, err := bitfield.SubtractBitField(toProve, good)
				if err != nil {
					return nil, xerrors.Errorf("toProve - good: %w", err)
				}

				sc, err := skipped.Count()
				if err != nil {
					return nil, xerrors.Errorf("getting skipped sector count: %w", err)
				}

				skipCount += sc

				ssi, err := s.sectorsForProof(ctx, good, partition.AllSectors, ts)
				if err != nil {
					return nil, xerrors.Errorf("getting sorted sector info: %w", err)
				}

				if len(ssi) == 0 {
					continue
				}

				sinfos = append(sinfos, ssi...)
				partitions = append(partitions, miner.PoStPartition{
					Index:   uint64(batchPartitionStartIdx + partIdx),
					Skipped: skipped,
				})
			}

			if len(sinfos) == 0 {
				// nothing to prove for this batch
				log.Info(s.PutLogf(di.Index, "no sector info for deadline:%d", di.Index))
				break
			}

			// Generate proof
			log.Info(s.PutLogw(di.Index, "running window post",
				"chain-random", rand,
				"deadline", di,
				"height", ts.Height(),
				"skipped", skipCount))

			tsStart := build.Clock.Now()

			mid, err := address.IDFromAddress(s.actor)
			if err != nil {
				return nil, err
			}
			pp := s.proofType
			// TODO: Drop after nv19 comes and goes
			if nv >= network.Version19 {
				pp, err = pp.ToV1_1PostProof()
				if err != nil {
					return nil, xerrors.Errorf("failed to convert to v1_1 post proof: %w", err)
				}
			}
			postOut, ps, err := s.prover.GenerateWindowPoSt(ctx, abi.ActorID(mid), pp, sinfos, append(abi.PoStRandomness{}, rand...))
			elapsed := time.Since(tsStart)
			log.Info(s.PutLogw(di.Index, "computing window post", "index", di.Index, "batch", batchIdx, "elapsed", elapsed, "rand", rand))

			if err == nil {
				// If we proved nothing, something is very wrong.
				if len(postOut) == 0 {
					err = xerrors.Errorf("received no proofs back from generate window post")
					return nil, err
				}

				clSectors := []proof.SectorInfo{}
				for _, si := range sinfos {
					clSectors = append(clSectors, proof.SectorInfo{
						SealProof:    si.ProofType,
						SectorNumber: si.ID.Number,
						SealedCID:    si.SealedCID,
					})
				}

				headTs, err := s.api.ChainHead(ctx)
				if err != nil {
					return nil, xerrors.Errorf("getting current head: %w", err)
				}

				checkRand, err := s.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), headTs.Key())
				if err != nil {
					return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
				}

				if !bytes.Equal(checkRand, rand) {
					log.Warn(s.PutLogw(di.Index, "windowpost randomness changed", "old", rand, "new", checkRand, "ts-height", ts.Height(), "challenge-height", di.Challenge, "tsk", ts.Key()))
					continue
				}

				// If we generated an incorrect proof, try again.
				if correct, err := s.verifier.VerifyWindowPoSt(ctx, proof.WindowPoStVerifyInfo{
					Randomness:        abi.PoStRandomness(checkRand),
					Proofs:            postOut,
					ChallengedSectors: clSectors,
					Prover:            abi.ActorID(mid),
				}); err != nil {
					log.Error(s.PutLogw(di.Index, "window post verification failed", "post", postOut, "error", err))
					time.Sleep(5 * time.Second)
					continue
				} else if !correct {
					log.Error(s.PutLogw(di.Index, "generated incorrect window post proof", "post", postOut, "error", err))
					continue
				}

				// Proof generation successful, stop retrying
				somethingToProve = true
				params.Partitions = partitions
				params.Proofs = postOut
				break
			}

			// Proof generation failed, so retry

			if len(ps) == 0 {
				// If we didn't skip any new sectors, we failed
				// for some other reason and we need to abort.
				err = xerrors.Errorf("running window post failed: %w", err)
				return nil, err
			}
			// TODO: maybe mark these as faulty somewhere?

			log.Warn(s.PutLogw(di.Index, "generate window post skipped sectors", "sectors", ps, "error", err, "try", retries))

			// Explicitly make sure we haven't aborted this PoSt
			// (GenerateWindowPoSt may or may not check this).
			// Otherwise, we could try to continue proving a
			// deadline after the deadline has ended.
			if ctx.Err() != nil {
				log.Warn(s.PutLogw(di.Index, "aborting PoSt due to context cancellation", "error", ctx.Err(), "deadline", di.Index))
				return nil, ctx.Err()
			}

			skipCount += uint64(len(ps))
			for _, sector := range ps {
				postSkipped.Set(uint64(sector.Number))
			}
		}

		// Nothing to prove for this batch, try the next batch
		if !somethingToProve {
			return nil, nil
		}
		return []miner.SubmitWindowedPoStParams{params}, nil
	}

	for batchIdx_p, batch_p := range partitionBatches {
		go func(batchIdx int, batch []api.Partition) {
			params, err := batchPoSt(batchIdx, batch)
			if err != nil {
				postChan <- errors.As(err)
			} else {
				postChan <- params
			}
		}(batchIdx_p, batch_p)
	}
	for i := len(partitionBatches); i > 0; i-- {
		select {
		case p := <-postChan:
			if p == nil {
				// no somthingToProve
				continue
			}
			err, ok := p.(error)
			if ok {
				log.Error(errors.As(err))
				continue
			}
			posts = append(posts, p.([]miner.SubmitWindowedPoStParams)...)
			continue

		case <-timech:
			log.Error(s.PutLogf(di.Index, "timeout for wdpost, index:%d, all:%d, success:%d", di.Index, len(partitionBatches), len(posts)))
			break
		}
	}
	return posts, nil
}
