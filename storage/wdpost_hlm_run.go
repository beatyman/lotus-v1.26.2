package storage

import (
	"bytes"
	"context"
	//"github.com/filecoin-project/lotus/chain/actors/builtin"
	"time"

	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/ipfs/go-cid"

	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

//	proof2 "github.com/filecoin-project/specs-actors/v2/actors/runtime/proof"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"

	"github.com/filecoin-project/lotus/chain/types"
)


func (s *WindowPoStScheduler) runHlmPost(ctx context.Context, di dline.Info, ts *types.TipSet) ([]miner.SubmitWindowedPoStParams, error) {
	log.Info("================================start generage wdpost========================================")
	defer func() {
		log.Info("================================End generage wdpost========================================")
	}()
	epochTime := int64(build.BlockDelaySecs)
	maxDelayEpoch := int64(di.Close-di.CurrentEpoch-10)
	maxDelayTime:= maxDelayEpoch*epochTime
	log.Info("max delay time:",maxDelayTime,"max delay epoch:",maxDelayEpoch,"epoch time:",epochTime,"time now:",time.Now().Unix())
	timech := time.After(time.Duration(maxDelayTime)*time.Second)
	ctx, span := trace.StartSpan(ctx, "storage.runPost")
	defer span.End()
	log.Info("deadline info:  index=",di.Index," current epoch=",di.CurrentEpoch," Challenge epoch = ",di.Challenge," PeriodStart=",di.PeriodStart," Open epoch=",di.Open," close epoch=",di.Close," ")
	log.Info("WPoStPeriodDeadlines:",di.WPoStPeriodDeadlines,"WPoStProvingPeriod=",di.WPoStPeriodDeadlines,"WPoStChallengeWindow=",di.WPoStChallengeWindow,"WPoStChallengeLookback=",di.WPoStChallengeLookback,"FaultDeclarationCutoff=",di.FaultDeclarationCutoff)
	go func() {
		// TODO: extract from runPost, run on fault cutoff boundaries

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

		if recoveries, sigmsg, err = s.checkNextRecoveries(context.TODO(), declDeadline, partitions, ts.Key()); err != nil {
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

		if faults, sigmsg, err = s.checkNextFaults(context.TODO(), declDeadline, partitions, ts.Key()); err != nil {
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

	rand, err := s.api.ChainGetRandomnessFromBeacon(ctx, ts.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes())
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
	}

	// Get the partitions for the given deadline
	partitions, err := s.api.StateMinerPartitions(ctx, s.actor, di.Index, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting partitions: %w", err)
	}
	log.Infof("DEBUG doPost StateMinerPartitions:%d,len:%d", di.Index, len(partitions))

	// Split partitions into batches, so as not to exceed the number of sectors
	// allowed in a single message
	partitionBatches, err := s.batchPartitions(partitions)
	if err != nil {
		return nil, err
	}

	// Generate proofs in batches
	posts := make([]miner.SubmitWindowedPoStParams, 0, len(partitionBatches))
	var count int =0
	postChan := make(chan miner.SubmitWindowedPoStParams)
	defer close(postChan)
	for batchIdx_p, batch_p := range partitionBatches {
		count++
		go func(batchIdx int,batch []api.Partition) {
			log.Info("lookup start batchIdx:",batchIdx)
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
						log.Error(xerrors.Errorf("removing faults from set of sectors to prove: %w", err))
						return
					}
					toProve, err = bitfield.MergeBitFields(toProve, partition.RecoveringSectors)
					if err != nil {
						log.Error(xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err))
						return
						//return nil, xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err)
					}

					good, err := s.checkSectors(ctx, toProve, ts.Key(), build.GetProvingCheckTimeout())
					if err != nil {
						log.Error(xerrors.Errorf("checking sectors to skip: %w", err))
						return
						//return nil, xerrors.Errorf("checking sectors to skip: %w", err)
					}

					good, err = bitfield.SubtractBitField(good, postSkipped)
					if err != nil {
						log.Error(xerrors.Errorf("toProve - postSkipped: %w", err))
						return
						//return nil, xerrors.Errorf("toProve - postSkipped: %w", err)
					}

					skipped, err := bitfield.SubtractBitField(toProve, good)
					if err != nil {
						log.Error(xerrors.Errorf("toProve - good: %w", err))
						return
						//return nil, xerrors.Errorf("toProve - good: %w", err)
					}

					sc, err := skipped.Count()
					if err != nil {
						log.Error(xerrors.Errorf("getting skipped sector count: %w", err))
						return
						//return nil, xerrors.Errorf("getting skipped sector count: %w", err)
					}

					skipCount += sc

					ssi, err := s.sectorsForProof(ctx, good, partition.AllSectors, ts)
					if err != nil {
						log.Error(xerrors.Errorf("getting sorted sector info: %w", err))
						return
						//return nil, xerrors.Errorf("getting sorted sector info: %w", err)
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
					log.Error(err)
					return
					//return nil, err
				}

				postOut, ps, err := s.prover.GenerateWindowPoSt(ctx, abi.ActorID(mid), sinfos, abi.PoStRandomness(rand))
				elapsed := time.Since(tsStart)

				log.Infow("computing window post", "index", di.Index, "batch", batchIdx, "elapsed", elapsed)

				if err == nil {
					if len(postOut) == 0 {
						log.Error(xerrors.Errorf("received no proofs back from generate window post"))
						return
						//return nil, xerrors.Errorf("received no proofs back from generate window post")
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
					log.Error(xerrors.Errorf("running window post failed: %w", err))
					return
					//return nil, xerrors.Errorf("running window post failed: %w", err)
				}
				// TODO: maybe mark these as faulty somewhere?

				log.Warnw("generate window post skipped sectors", "sectors", ps, "error", err, "try", retries)

				// Explicitly make sure we haven't aborted this PoSt
				// (GenerateWindowPoSt may or may not check this).
				// Otherwise, we could try to continue proving a
				// deadline after the deadline has ended.
				if ctx.Err() != nil {
					log.Warnw("aborting PoSt due to context cancellation", "error", ctx.Err(), "deadline", di.Index)
					log.Error(ctx.Err())
					return
					//return nil, ctx.Err()
				}

				skipCount += uint64(len(ps))
				for _, sector := range ps {
					postSkipped.Set(uint64(sector.Number))
				}
			}

			// Nothing to prove for this batch, try the next batch
			if !somethingToProve {
				return
			}
			log.Info("wdpost proof successfully:",batchPartitionStartIdx)
			postChan <- params
			//posts = append(posts, params)
		}(batchIdx_p, batch_p)
	}
	for{
		select{
		case param := <- postChan:
			count--
			posts = append(posts, param)
			if count==0{
				return posts,nil
			}
		case <- timech:
			log.Warn("timeout for wdpost:",di.Index)
			return posts,nil
		}
	}
	return posts, nil
}
