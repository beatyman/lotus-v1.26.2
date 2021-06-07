package storage

import (
	"bytes"
	"context"
	"time"

	"github.com/gwaylib/errors"

	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/ipfs/go-cid"

	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/specs-actors/v3/actors/runtime/proof"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"

	"github.com/filecoin-project/lotus/chain/types"
)

func (s *WindowPoStScheduler) runHlmPoStCycle(ctx context.Context, di dline.Info, ts *types.TipSet) ([]miner.SubmitWindowedPoStParams, error) {
	log.Info("================================ DEBUG: Start generage wdpost========================================")
	defer log.Info("================================DEBUG: End generage wdpost========================================")
	epochTime := int64(build.BlockDelaySecs)
	maxDelayEpoch := int64(di.Close - di.CurrentEpoch - 10)
	maxDelayTime := maxDelayEpoch * epochTime
	log.Infow("max delay time:", maxDelayTime, "max delay epoch:", maxDelayEpoch, "epoch time:", epochTime)
	timech := time.After(time.Duration(maxDelayTime) * time.Second)
	log.Infow("deadline info:  index=", di.Index, " current epoch=", di.CurrentEpoch, " Challenge epoch = ", di.Challenge, " PeriodStart=", di.PeriodStart, " Open epoch=", di.Open, " close epoch=", di.Close, " ")
	log.Infow("WPoStPeriodDeadlines:", di.WPoStPeriodDeadlines, "WPoStProvingPeriod=", di.WPoStPeriodDeadlines, "WPoStChallengeWindow=", di.WPoStChallengeWindow, "WPoStChallengeLookback=", di.WPoStChallengeLookback, "FaultDeclarationCutoff=", di.FaultDeclarationCutoff)

	ctx, span := trace.StartSpan(ctx, "storage.runPoStCycle")
	defer span.End()

	go func() {
		// TODO: extract from runPoStCycle, run on fault cutoff boundaries

		// check faults / recoveries for the *next* deadline. It's already too
		// late to declare them for this deadline
		declDeadline := (di.Index + 2) % di.WPoStPeriodDeadlines

		partitions, err := s.api.StateMinerPartitions(context.TODO(), s.actor, declDeadline, ts.Key())
		if err != nil {
			log.Errorf("getting partitions: %v", err)
			return
		}

		var (
			sigmsg     *types.SignedMessage
			recoveries []miner.RecoveryDeclaration
			faults     []miner.FaultDeclaration

			// optionalCid returns the CID of the message, or cid.Undef is the
			// message is nil. We don't need the argument (could capture the
			// pointer), but it's clearer and purer like that.
			optionalCid = func(sigmsg *types.SignedMessage) cid.Cid {
				if sigmsg == nil {
					return cid.Undef
				}
				return sigmsg.Cid()
			}
		)

		if recoveries, sigmsg, err = s.declareRecoveries(context.TODO(), declDeadline, partitions, ts.Key()); err != nil {
			// TODO: This is potentially quite bad, but not even trying to post when this fails is objectively worse
			log.Errorf("checking sector recoveries: %v", err)
		}

		s.journal.RecordEvent(s.evtTypes[evtTypeWdPoStRecoveries], func() interface{} {
			j := WdPoStRecoveriesProcessedEvt{
				evtCommon:    s.getEvtCommon(err),
				Declarations: recoveries,
				MessageCID:   optionalCid(sigmsg),
			}
			j.Error = err
			return j
		})

		if ts.Height() > build.UpgradeIgnitionHeight {
			return // FORK: declaring faults after ignition upgrade makes no sense
		}

		if faults, sigmsg, err = s.declareFaults(context.TODO(), declDeadline, partitions, ts.Key()); err != nil {
			// TODO: This is also potentially really bad, but we try to post anyways
			log.Errorf("checking sector faults: %v", err)
		}

		s.journal.RecordEvent(s.evtTypes[evtTypeWdPoStFaults], func() interface{} {
			return WdPoStFaultsProcessedEvt{
				evtCommon:    s.getEvtCommon(err),
				Declarations: faults,
				MessageCID:   optionalCid(sigmsg),
			}
		})
	}()

	buf := new(bytes.Buffer)
	if err := s.actor.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	headTs, err := s.api.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting current head: %w", err)
	}

	// TODO: 重新测试以便确认是否每一个单独的分区证明都需要一个不同的随机数
	rand, err := s.api.ChainGetRandomnessFromBeacon(ctx, headTs.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes())
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
	partitionBatches, err := s.batchPartitions(partitions, nv)
	if err != nil {
		return nil, err
	}

	// Generate proofs in batches
	posts := make([]miner.SubmitWindowedPoStParams, 0, len(partitionBatches))
	postChan := make(chan interface{}, 1)
	defer close(postChan)

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

		// Retry until we run out of sectors to prove.
		for retries := 0; ; retries++ {
			var partitions []miner.PoStPartition
			var sinfos []storage.ProofSectorInfo
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

				good, err := s.checkSectors(ctx, toProve, ts.Key(), build.GetProvingCheckTimeout())
				if err != nil {
					return nil, xerrors.Errorf("checking sectors to skip: %w", err)
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
				log.Infof("no sector info for deadline:%d", di.Index)
				break
			}

			// Generate proof
			log.Infow("running window post",
				"chain-random", rand,
				"deadline", di,
				"height", ts.Height(),
				"skipped", skipCount)

			tsStart := build.Clock.Now()

			mid, err := address.IDFromAddress(s.actor)
			if err != nil {
				return nil, err
			}

			postOut, ps, err := s.prover.GenerateWindowPoSt(ctx, abi.ActorID(mid), sinfos, append(abi.PoStRandomness{}, rand...))
			elapsed := time.Since(tsStart)

			log.Infow("computing window post", "index", di.Index, "batch", batchIdx, "elapsed", elapsed, "rand", rand)

			if err == nil {
				// If we proved nothing, something is very wrong.
				if len(postOut) == 0 {
					return nil, xerrors.Errorf("received no proofs back from generate window post")
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

				checkRand, err := s.api.ChainGetRandomnessFromBeacon(ctx, headTs.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes())
				if err != nil {
					return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
				}

				if !bytes.Equal(checkRand, rand) {
					log.Warnw("windowpost randomness changed", "old", rand, "new", checkRand, "ts-height", ts.Height(), "challenge-height", di.Challenge, "tsk", ts.Key())
					continue
				}

				// If we generated an incorrect proof, try again.
				if correct, err := s.verifier.VerifyWindowPoSt(ctx, proof.WindowPoStVerifyInfo{
					Randomness:        abi.PoStRandomness(checkRand),
					Proofs:            postOut,
					ChallengedSectors: clSectors,
					Prover:            abi.ActorID(mid),
				}); err != nil {
					log.Errorw("window post verification failed", "post", postOut, "error", err)
					time.Sleep(5 * time.Second)
					continue
				} else if !correct {
					log.Errorw("generated incorrect window post proof", "post", postOut, "error", err)
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
				return nil, xerrors.Errorf("running window post failed: %w", err)
			}
			// TODO: maybe mark these as faulty somewhere?

			log.Warnw("generate window post skipped sectors", "sectors", ps, "error", err, "try", retries)

			// Explicitly make sure we haven't aborted this PoSt
			// (GenerateWindowPoSt may or may not check this).
			// Otherwise, we could try to continue proving a
			// deadline after the deadline has ended.
			if ctx.Err() != nil {
				log.Warnw("aborting PoSt due to context cancellation", "error", ctx.Err(), "deadline", di.Index)
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
		go func() {
			params, err := batchPoSt(batchIdx_p, batch_p)
			if err != nil {
				postChan <- errors.As(err)
			} else {
				postChan <- params
			}
		}()
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
			log.Errorf("timeout for wdpost, index:%d, all:%d, success:%d", di.Index, len(partitionBatches), len(posts))
			break
		}
	}
	return posts, nil
}
