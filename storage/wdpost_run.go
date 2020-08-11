package storage

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/filecoin-project/go-bitfield"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

var errNoPartitions = errors.New("no partitions")

func (s *WindowPoStScheduler) failPost(deadline *miner.DeadlineInfo) {
	log.Errorf("TODO")
	/*s.failLk.Lock()
	if eps > s.failed {
		s.failed = eps
	}
	s.failLk.Unlock()*/
}

func (s *WindowPoStScheduler) doPost(ctx context.Context, deadline *miner.DeadlineInfo, ts *types.TipSet) {
	ctx, abort := context.WithCancel(ctx)

	s.abort = abort
	s.activeDeadline = deadline

	go func() {
		defer abort()

		ctx, span := trace.StartSpan(ctx, "WindowPoStScheduler.doPost")
		defer span.End()

		proof, err := s.runPost(ctx, *deadline, ts)
		switch err {
		case errNoPartitions:
			return
		case nil:
			if err := s.submitPost(ctx, proof); err != nil {
				log.Errorf("submitPost failed: %+v", err)
				s.failPost(deadline)
				return
			}
		default:
			log.Errorf("runPost failed: %+v", err)
			s.failPost(deadline)
			return
		}
	}()
}

func (s *WindowPoStScheduler) checkSectors(ctx context.Context, check *abi.BitField) (*abi.BitField, error) {
	spt, err := s.proofType.RegisteredSealProof()
	if err != nil {
		return nil, xerrors.Errorf("getting seal proof type: %w", err)
	}

	mid, err := address.IDFromAddress(s.actor)
	if err != nil {
		return nil, err
	}

	sectors := make(map[abi.SectorID]struct{})
	var tocheck []abi.SectorID
	err = check.ForEach(func(snum uint64) error {
		s := abi.SectorID{
			Miner:  abi.ActorID(mid),
			Number: abi.SectorNumber(snum),
		}

		tocheck = append(tocheck, s)
		sectors[s] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, xerrors.Errorf("iterating over bitfield: %w", err)
	}

	bad, err := s.faultTracker.CheckProvable(ctx, spt, tocheck)
	if err != nil {
		return nil, xerrors.Errorf("checking provable sectors: %w", err)
	}
	for _, id := range bad {
		delete(sectors, id)
	}

	log.Warnw("Checked sectors", "checked", len(tocheck), "good", len(sectors))

	sbf := bitfield.New()
	for s := range sectors {
		(&sbf).Set(uint64(s.Number))
	}

	return &sbf, nil
}

func (s *WindowPoStScheduler) checkNextRecoveries(ctx context.Context, dlIdx uint64, partitions []*miner.Partition) error {
	ctx, span := trace.StartSpan(ctx, "storage.checkNextRecoveries")
	defer span.End()

	params := &miner.DeclareFaultsRecoveredParams{
		Recoveries: []miner.RecoveryDeclaration{},
	}

	faulty := uint64(0)

	for partIdx, partition := range partitions {
		unrecovered, err := bitfield.SubtractBitField(partition.Faults, partition.Recoveries)
		if err != nil {
			return xerrors.Errorf("subtracting recovered set from fault set: %w", err)
		}

		uc, err := unrecovered.Count()
		if err != nil {
			return xerrors.Errorf("counting unrecovered sectors: %w", err)
		}

		if uc == 0 {
			continue
		}

		faulty += uc

		recovered, err := s.checkSectors(ctx, unrecovered)
		if err != nil {
			return xerrors.Errorf("checking unrecovered sectors: %w", err)
		}

		// if all sectors failed to recover, don't declare recoveries
		recoveredCount, err := recovered.Count()
		if err != nil {
			return xerrors.Errorf("counting recovered sectors: %w", err)
		}

		if recoveredCount == 0 {
			continue
		}

		params.Recoveries = append(params.Recoveries, miner.RecoveryDeclaration{
			Deadline:  dlIdx,
			Partition: uint64(partIdx),
			Sectors:   recovered,
		})
	}

	if len(params.Recoveries) == 0 {
		if faulty != 0 {
			log.Warnw("No recoveries to declare", "deadline", dlIdx, "faulty", faulty)
		}

		return nil
	}

	enc, aerr := actors.SerializeParams(params)
	if aerr != nil {
		return xerrors.Errorf("could not serialize declare recoveries parameters: %w", aerr)
	}
	if s.noSubmit {
		log.Infow("noSubmit for DeclareFaultsRecovered", "deadline", dlIdx)
		return nil
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.DeclareFaultsRecovered,
		Params: enc,
		Value:  types.NewInt(0),
	}

	sm, err := s.api.MpoolPushMessage(ctx, msg)
	if err != nil {
		return xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Warnw("declare faults recovered Message CID", "deadline", dlIdx, "cid", sm.Cid())

	rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
	if err != nil {
		return xerrors.Errorf("declare faults recovered wait error: %w", err)
	}

	if rec.Receipt.ExitCode != 0 {
		log.Infow("declare faults recovered exit non-0 ", rec.Receipt.ExitCode, "deadline", dlIdx, "cid", sm.Cid())
		return xerrors.Errorf("declare faults recovered wait non-0 exit code: %d", rec.Receipt.ExitCode)
	}
	log.Infow("declare faults recovered exit 0", "deadline", dlIdx, "cid", sm.Cid())

	return nil
}

func (s *WindowPoStScheduler) checkNextFaults(ctx context.Context, dlIdx uint64, partitions []*miner.Partition) error {
	ctx, span := trace.StartSpan(ctx, "storage.checkNextFaults")
	defer span.End()

	params := &miner.DeclareFaultsParams{
		Faults: []miner.FaultDeclaration{},
	}

	bad := uint64(0)

	for partIdx, partition := range partitions {
		toCheck, err := partition.ActiveSectors()
		if err != nil {
			return xerrors.Errorf("getting active sectors: %w", err)
		}

		good, err := s.checkSectors(ctx, toCheck)
		if err != nil {
			return xerrors.Errorf("checking sectors: %w", err)
		}

		faulty, err := bitfield.SubtractBitField(toCheck, good)
		if err != nil {
			return xerrors.Errorf("calculating faulty sector set: %w", err)
		}

		c, err := faulty.Count()
		if err != nil {
			return xerrors.Errorf("counting faulty sectors: %w", err)
		}

		if c == 0 {
			continue
		}

		bad += c

		params.Faults = append(params.Faults, miner.FaultDeclaration{
			Deadline:  dlIdx,
			Partition: uint64(partIdx),
			Sectors:   faulty,
		})
	}

	if len(params.Faults) == 0 {
		return nil
	}

	log.Errorw("DETECTED FAULTY SECTORS, declaring faults", "count", bad)

	enc, aerr := actors.SerializeParams(params)
	if aerr != nil {
		return xerrors.Errorf("could not serialize declare faults parameters: %w", aerr)
	}
	if s.noSubmit {
		log.Infow("noSubmit for DeclareFaults", "deadline", dlIdx)
		return nil
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.DeclareFaults,
		Params: enc,
		Value:  types.NewInt(0), // TODO: Is there a fee?
	}

	sm, err := s.api.MpoolPushMessage(ctx, msg)
	if err != nil {
		return xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Warnw("declare faults Message CID", "deadline", dlIdx, "cid", sm.Cid())

	rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
	if err != nil {
		return xerrors.Errorf("declare faults wait error: %w", err)
	}

	if rec.Receipt.ExitCode != 0 {
		return xerrors.Errorf("declare faults wait non-0 exit code: %d", rec.Receipt.ExitCode)
	}
	log.Infow("declare faults recovered exit 0", "deadline", dlIdx, "cid", sm.Cid())

	return nil
}

func (s *WindowPoStScheduler) runPost(ctx context.Context, di miner.DeadlineInfo, ts *types.TipSet) (*miner.SubmitWindowedPoStParams, error) {
	ctx, span := trace.StartSpan(ctx, "storage.runPost")
	defer span.End()

	go func() {
		// TODO: extract from runPost, run on fault cutoff boundaries

		// check faults / recoveries for the *next* deadline. It's already too
		// late to declare them for this deadline
		declDeadline := (di.Index + 2) % miner.WPoStPeriodDeadlines

		partitions, err := s.api.StateMinerPartitions(context.TODO(), s.actor, declDeadline, ts.Key())
		if err != nil {
			log.Errorf("getting partitions: %v", err)
			return
		}

		if err := s.checkNextRecoveries(context.TODO(), declDeadline, partitions); err != nil {
			// TODO: This is potentially quite bad, but not even trying to post when this fails is objectively worse
			log.Errorf("checking sector recoveries: %v", err)
		}

		if err := s.checkNextFaults(context.TODO(), declDeadline, partitions); err != nil {
			// TODO: This is also potentially really bad, but we try to post anyways
			log.Errorf("checking sector faults: %v", err)
		}
	}()

	buf := new(bytes.Buffer)
	if err := s.actor.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}
	rand, err := s.api.ChainGetRandomness(ctx, ts.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes())
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness for windowPost (ts=%d; deadline=%d): %w", ts.Height(), di, err)
	}

	partitions, err := s.api.StateMinerPartitions(ctx, s.actor, di.Index, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting partitions: %w", err)
	}
	log.Infof("DEBUG doPost StateMinerPartitions:%d,len:%d", di.Index, len(partitions))

	params := &miner.SubmitWindowedPoStParams{
		Deadline:   di.Index,
		Partitions: make([]miner.PoStPartition, 0, len(partitions)),
		Proofs:     nil,
	}

	var sinfos []abi.SectorInfo
	sidToPart := map[abi.SectorNumber]uint64{}
	skipCount := uint64(0)

	for partIdx, partition := range partitions {
		// TODO: Can do this in parallel
		toProve, err := partition.ActiveSectors()
		if err != nil {
			log.Warn(err)
			return nil, xerrors.Errorf("getting active sectors: %w", err)
		}
		toProveCount, err := toProve.Count()
		if err != nil {
			log.Warn(err)
		}
		recoverCount, err := partition.Recoveries.Count()
		if err != nil {
			log.Warn(err)
		}

		tsc, err := partition.Sectors.Count()
		if err != nil {
			log.Warn(err)
		}
		tfc, err := partition.Faults.Count()
		if err != nil {
			log.Warn(err)
		}
		ttc, err := partition.Terminated.Count()
		if err != nil {
			log.Warn(err)
		}

		log.Infof("DEBUG doPost StateMinerPartitions, index:%d,toProve:%d, recovers:%d,tsc:%d,tfc:%d,ttc:%d", partIdx, toProveCount, recoverCount, tsc, tfc, ttc)

		toProve, err = bitfield.MergeBitFields(toProve, partition.Recoveries)
		if err != nil {
			log.Warn(err)
			return nil, xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err)
		}

		good, err := s.checkSectors(ctx, toProve)
		if err != nil {
			log.Warn(err)
			return nil, xerrors.Errorf("checking sectors to skip: %w", err)
		}
		goodCount, _ := good.Count()
		log.Infof("DEBUG doPost StateMinerPartitions, index:%d,good:%d", partIdx, goodCount)

		skipped, err := bitfield.SubtractBitField(toProve, good)
		if err != nil {
			return nil, xerrors.Errorf("toProve - good: %w", err)
		}
		skippedCount, _ := skipped.Count()
		log.Infof("DEBUG doPost StateMinerPartitions, index:%d,skipped:%d", partIdx, skippedCount)

		sc, err := skipped.Count()
		if err != nil {
			return nil, xerrors.Errorf("getting skipped sector count: %w", err)
		}

		skipCount += sc

		ssi, err := s.sectorInfo(ctx, good, ts)
		if err != nil {
			return nil, xerrors.Errorf("getting sorted sector info: %w", err)
		}
		log.Infof("DEBUG doPost StateMinerPartitions, index:%d,len ssi:%d", partIdx, len(ssi))

		if len(ssi) == 0 {
			continue
		}

		sinfos = append(sinfos, ssi...)
		for _, si := range ssi {
			sidToPart[si.SectorNumber] = uint64(partIdx)
		}

		params.Partitions = append(params.Partitions, miner.PoStPartition{
			Index:   uint64(partIdx),
			Skipped: skipped,
		})
	}

	if len(sinfos) == 0 {
		log.Info("NoPartitions")
		// nothing to prove..
		return nil, errNoPartitions
	}

	log.Infow("running windowPost",
		"chain-random", rand,
		"deadline", di,
		"height", ts.Height(),
		"skipped", skipCount)

	tsStart := build.Clock.Now()

	log.Infow("generating windowPost", "sectors", len(sinfos))

	mid, err := address.IDFromAddress(s.actor)
	if err != nil {
		return nil, err
	}

	postOut, postSkipped, err := s.prover.GenerateWindowPoSt(ctx, abi.ActorID(mid), sinfos, abi.PoStRandomness(rand))
	if err != nil {
		return nil, xerrors.Errorf("running post failed: %w", err)
	}

	if len(postOut) == 0 {
		return nil, xerrors.Errorf("received proofs back from generate window post")
	}

	params.Proofs = postOut

	for _, sector := range postSkipped {
		params.Partitions[sidToPart[sector.Number]].Skipped.Set(uint64(sector.Number))
	}

	elapsed := time.Since(tsStart)
	log.Infow("submitting window PoSt", "deadline", di.Index, "elapsed", elapsed)

	return params, nil
}

func (s *WindowPoStScheduler) sectorInfo(ctx context.Context, deadlineSectors *abi.BitField, ts *types.TipSet) ([]abi.SectorInfo, error) {
	sset, err := s.api.StateMinerSectors(ctx, s.actor, deadlineSectors, false, ts.Key())
	if err != nil {
		return nil, err
	}

	sbsi := make([]abi.SectorInfo, len(sset))
	for k, sector := range sset {
		sbsi[k] = abi.SectorInfo{
			SectorNumber: sector.ID,
			SealedCID:    sector.Info.SealedCID,
			SealProof:    sector.Info.SealProof,
		}
	}

	return sbsi, nil
}

func (s *WindowPoStScheduler) submitPost(ctx context.Context, proof *miner.SubmitWindowedPoStParams) error {
	ctx, span := trace.StartSpan(ctx, "storage.commitPost")
	defer span.End()

	enc, aerr := actors.SerializeParams(proof)
	if aerr != nil {
		return xerrors.Errorf("could not serialize submit post parameters: %w", aerr)
	}

	if s.noSubmit {
		log.Info("noSubmit for SubmitWindowedPoSt")
		return nil
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.SubmitWindowedPoSt,
		Params: enc,
		Value:  types.NewInt(1000), // currently hard-coded late fee in actor, returned if not late
	}

	// TODO: consider maybe caring about the output
	sm, err := s.api.MpoolPushMessage(ctx, msg)
	if err != nil {
		return xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Infof("Submitted window post: %s", sm.Cid())

	go func() {
		rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
		if err != nil {
			log.Error(err)
			return
		}

		if rec.Receipt.ExitCode == 0 {
			log.Infof("Submitting window post %s success.", sm.Cid())
			return
		}

		log.Errorf("Submitting window post %s failed: exit %d", sm.Cid(), rec.Receipt.ExitCode)
	}()

	return nil
}
