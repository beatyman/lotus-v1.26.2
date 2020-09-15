package storage

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/gwaylib/errors"

	"github.com/filecoin-project/go-state-types/dline"

	"github.com/filecoin-project/specs-actors/actors/runtime/proof"

	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/lotus/api"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/ipfs/go-cid"

	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/journal"
)

var errNoPartitions = errors.New("no partitions")

func (s *WindowPoStScheduler) failPost(err error, deadline *dline.Info) {
	journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStScheduler], func() interface{} {
		return WdPoStSchedulerEvt{
			evtCommon: s.getEvtCommon(err),
			State:     SchedulerStateFaulted,
		}
	})

	log.Errorf("TODO")
	/*s.failLk.Lock()
	if eps > s.failed {
		s.failed = eps
	}
	s.failLk.Unlock()*/
}

func (s *WindowPoStScheduler) doPost(ctx context.Context, submit bool, deadline *dline.Info, ts *types.TipSet) {
	ctx, abort := context.WithCancel(ctx)

	s.abort = abort
	s.activeDeadline = deadline

	journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStScheduler], func() interface{} {
		return WdPoStSchedulerEvt{
			evtCommon: s.getEvtCommon(nil),
			State:     SchedulerStateStarted,
		}
	})

	go func() {
		defer abort()

		ctx, span := trace.StartSpan(ctx, "WindowPoStScheduler.doPost")
		defer span.End()

		// recordProofsEvent records a successful proofs_processed event in the
		// journal, even if it was a noop (no partitions).
		recordProofsEvent := func(partitions []miner.PoStPartition, mcid cid.Cid) {
			journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStProofs], func() interface{} {
				return &WdPoStProofsProcessedEvt{
					evtCommon:  s.getEvtCommon(nil),
					Partitions: partitions,
					MessageCID: mcid,
				}
			})
		}

		proof, err := s.runPost(ctx, submit, *deadline, ts)
		switch err {
		case errNoPartitions:
			recordProofsEvent(nil, cid.Undef)
			return
		case nil:
			sm, err := s.submitPost(ctx, proof)
			if err != nil {
				log.Errorf("submitPost failed: %+v", err)
				s.failPost(err, deadline)
				return
			}
			recordProofsEvent(proof.Partitions, sm.Cid())
		default:
			log.Errorf("runPost failed: %+v", err)
			s.failPost(err, deadline)
			return
		}

		journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStScheduler], func() interface{} {
			return WdPoStSchedulerEvt{
				evtCommon: s.getEvtCommon(nil),
				State:     SchedulerStateSucceeded,
			}
		})
	}()
}

func (s *WindowPoStScheduler) checkSectors(ctx context.Context, check bitfield.BitField, timeout time.Duration) (bitfield.BitField, error) {
	spt, err := s.proofType.RegisteredSealProof()
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("getting seal proof type: %w", err)
	}

	mid, err := address.IDFromAddress(s.actor)
	if err != nil {
		return bitfield.BitField{}, err
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
		return bitfield.BitField{}, xerrors.Errorf("iterating over bitfield: %w", err)
	}

	all, _, err := s.faultTracker.CheckProvable(ctx, spt, tocheck, timeout)
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("checking provable sectors: %w", err)
	}
	// bad
	for _, val := range all {
		if val.Err != nil {
			log.Warnf("s-t0%d-%d,%d,%s,%s", val.ID.Miner, val.ID.Number, val.Used, val.Used.String(), errors.ParseError(val.Err))
			delete(sectors, val.ID)
		}
	}

	log.Warnw("Checked sectors", "checked", len(tocheck), "good", len(sectors))

	sbf := bitfield.New()
	for s := range sectors {
		sbf.Set(uint64(s.Number))
	}

	return sbf, nil
}

func (s *WindowPoStScheduler) checkNextRecoveries(ctx context.Context, submit bool, dlIdx uint64, partitions []*miner.Partition) ([]miner.RecoveryDeclaration, *types.SignedMessage, error) {
	ctx, span := trace.StartSpan(ctx, "storage.checkNextRecoveries")
	defer span.End()

	faulty := uint64(0)
	params := &miner.DeclareFaultsRecoveredParams{
		Recoveries: []miner.RecoveryDeclaration{},
	}

	for partIdx, partition := range partitions {
		unrecovered, err := bitfield.SubtractBitField(partition.Faults, partition.Recoveries)
		if err != nil {
			return nil, nil, xerrors.Errorf("subtracting recovered set from fault set: %w", err)
		}

		uc, err := unrecovered.Count()
		if err != nil {
			return nil, nil, xerrors.Errorf("counting unrecovered sectors: %w", err)
		}

		if uc == 0 {
			continue
		}

		faulty += uc

		recovered, err := s.checkSectors(ctx, unrecovered, 60*time.Second)
		if err != nil {
			return nil, nil, xerrors.Errorf("checking unrecovered sectors: %w", err)
		}

		// if all sectors failed to recover, don't declare recoveries
		recoveredCount, err := recovered.Count()
		if err != nil {
			return nil, nil, xerrors.Errorf("counting recovered sectors: %w", err)
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

	recoveries := params.Recoveries
	if len(recoveries) == 0 {
		if faulty != 0 {
			log.Warnw("No recoveries to declare", "deadline", dlIdx, "faulty", faulty)
		}

		return recoveries, nil, nil
	}

	enc, aerr := actors.SerializeParams(params)
	if aerr != nil {
		return recoveries, nil, xerrors.Errorf("could not serialize declare recoveries parameters: %w", aerr)
	}
	if !submit {
		log.Infow("noSubmit for DeclareFaultsRecovered", "deadline", dlIdx)
		return recoveries, nil, nil
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.DeclareFaultsRecovered,
		Params: enc,
		Value:  types.NewInt(0),
	}
	spec := &api.MessageSendSpec{MaxFee: abi.TokenAmount(s.feeCfg.MaxWindowPoStGasFee)}
	s.setSender(ctx, msg, spec)

	sm, err := s.api.MpoolPushMessage(ctx, msg, &api.MessageSendSpec{MaxFee: abi.TokenAmount(s.feeCfg.MaxWindowPoStGasFee)})
	if err != nil {
		return recoveries, sm, xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Warnw("declare faults recovered Message CID", "deadline", dlIdx, "cid", sm.Cid())

	rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
	if err != nil {
		return recoveries, sm, xerrors.Errorf("declare faults recovered wait error: %w", err)
	}

	if rec.Receipt.ExitCode != 0 {
		log.Infow("declare faults recovered exit non-0 ", rec.Receipt.ExitCode, "deadline", dlIdx, "cid", sm.Cid())
		return recoveries, sm, xerrors.Errorf("declare faults recovered wait non-0 exit code: %d", rec.Receipt.ExitCode)
	}
	log.Infow("declare faults recovered exit 0", "deadline", dlIdx, "cid", sm.Cid())

	return recoveries, sm, nil
}

func (s *WindowPoStScheduler) checkNextFaults(ctx context.Context, dlIdx uint64, partitions []*miner.Partition) ([]miner.FaultDeclaration, *types.SignedMessage, error) {
	ctx, span := trace.StartSpan(ctx, "storage.checkNextFaults")
	defer span.End()

	bad := uint64(0)
	params := &miner.DeclareFaultsParams{
		Faults: []miner.FaultDeclaration{},
	}

	for partIdx, partition := range partitions {
		toCheck, err := partition.ActiveSectors()
		if err != nil {
			return nil, nil, xerrors.Errorf("getting active sectors: %w", err)
		}

		good, err := s.checkSectors(ctx, toCheck, 60*time.Second)
		if err != nil {
			return nil, nil, xerrors.Errorf("checking sectors: %w", err)
		}

		faulty, err := bitfield.SubtractBitField(toCheck, good)
		if err != nil {
			return nil, nil, xerrors.Errorf("calculating faulty sector set: %w", err)
		}

		c, err := faulty.Count()
		if err != nil {
			return nil, nil, xerrors.Errorf("counting faulty sectors: %w", err)
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

	faults := params.Faults
	if len(faults) == 0 {
		return faults, nil, nil
	}

	log.Errorw("DETECTED FAULTY SECTORS, declaring faults", "count", bad)

	enc, aerr := actors.SerializeParams(params)
	if aerr != nil {
		return faults, nil, xerrors.Errorf("could not serialize declare faults parameters: %w", aerr)
	}
	if s.noSubmit {
		log.Infow("noSubmit for DeclareFaults", "deadline", dlIdx)
		return faults, nil, nil
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.DeclareFaults,
		Params: enc,
		Value:  types.NewInt(0), // TODO: Is there a fee?
	}
	spec := &api.MessageSendSpec{MaxFee: abi.TokenAmount(s.feeCfg.MaxWindowPoStGasFee)}
	s.setSender(ctx, msg, spec)

	sm, err := s.api.MpoolPushMessage(ctx, msg, spec)
	if err != nil {
		return faults, sm, xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Warnw("declare faults Message CID", "deadline", dlIdx, "cid", sm.Cid())

	rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
	if err != nil {
		return faults, sm, xerrors.Errorf("declare faults wait error: %w", err)
	}

	if rec.Receipt.ExitCode != 0 {
		return faults, sm, xerrors.Errorf("declare faults wait non-0 exit code: %d", rec.Receipt.ExitCode)
	}
	log.Infow("declare faults recovered exit 0", "deadline", dlIdx, "cid", sm.Cid())

	return faults, sm, nil
}

func (s *WindowPoStScheduler) runPost(ctx context.Context, submit bool, di dline.Info, ts *types.TipSet) (*miner.SubmitWindowedPoStParams, error) {
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

		if recoveries, sigmsg, err = s.checkNextRecoveries(context.TODO(), submit, declDeadline, partitions); err != nil {
			// TODO: This is potentially quite bad, but not even trying to post when this fails is objectively worse
			log.Errorf("checking sector recoveries: %v", err)
		}

		journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStRecoveries], func() interface{} {
			j := WdPoStRecoveriesProcessedEvt{
				evtCommon:    s.getEvtCommon(err),
				Declarations: recoveries,
				MessageCID:   optionalCid(sigmsg),
			}
			j.Error = err
			return j
		})

		if faults, sigmsg, err = s.checkNextFaults(context.TODO(), declDeadline, partitions); err != nil {
			// TODO: This is also potentially really bad, but we try to post anyways
			log.Errorf("checking sector faults: %v", err)
		}

		journal.J.RecordEvent(s.evtTypes[evtTypeWdPoStFaults], func() interface{} {
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

	var sinfos []proof.SectorInfo
	var sinfosLk = sync.Mutex{}
	sidToPart := map[abi.SectorNumber]int{}
	skipCount := uint64(0)
	done := make(chan error, len(partitions))
	parallelDo := func(partIdx int, partition *miner.Partition) {
		var gErr error
		defer func() {
			done <- gErr
		}()
		// format for vimdiff
		{
			toProve, err := partition.ActiveSectors()
			if err != nil {
				gErr = errors.As(err, partIdx)
				return
			}

			toProve, err = bitfield.MergeBitFields(toProve, partition.Recoveries)
			if err != nil {
				gErr = xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err)
				return
			}

			good, err := s.checkSectors(ctx, toProve, 3*time.Second)
			if err != nil {
				gErr = xerrors.Errorf("checking sectors to skip: %w", err)
				return
			}

			skipped, err := bitfield.SubtractBitField(toProve, good)
			if err != nil {
				gErr = errors.As(err, partIdx)
				return
			}

			sc, err := skipped.Count()
			if err != nil {
				gErr = errors.As(err, partIdx)
				return
			}

			skipCount += sc

			ssi, err := s.sectorsForProof(ctx, good, partition.Sectors, ts)
			if err != nil {
				gErr = xerrors.Errorf("getting sorted sector info: %w", err)
				return
			}

			if len(ssi) == 0 {
				return
			}

			sinfosLk.Lock()
			sinfos = append(sinfos, ssi...)
			for _, si := range ssi {
				sidToPart[si.SectorNumber] = partIdx
			}

			params.Partitions = append(params.Partitions, miner.PoStPartition{
				Index:   uint64(partIdx),
				Skipped: skipped,
			})
			sinfosLk.Unlock()
		}
	}
	for partIdx, partition := range partitions {
		//  Can do this in parallel
		go parallelDo(partIdx, partition)
	}
	for waits := len(partitions); waits > 0; waits-- {
		err := <-done
		if err != nil {
			log.Warn(errors.As(err))
			return nil, err
		}
	}

	// format for vimdiff
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

	mid, err := address.IDFromAddress(s.actor)
	if err != nil {
		return nil, err
	}

	postOut, postSkipped, err := s.prover.GenerateWindowPoSt(ctx, abi.ActorID(mid), sinfos, abi.PoStRandomness(rand))
	if err != nil {
		return nil, xerrors.Errorf("running post failed: %w", err)
	}

	if len(postOut) == 0 {
		return nil, xerrors.Errorf("received no proofs back from generate window post")
	}

	params.Proofs = postOut

	for _, sector := range postSkipped {
		params.Partitions[sidToPart[sector.Number]].Skipped.Set(uint64(sector.Number))
	}

	commEpoch := di.Open
	commRand, err := s.api.ChainGetRandomnessFromTickets(ctx, ts.Key(), crypto.DomainSeparationTag_PoStChainCommit, commEpoch, nil)
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness for windowPost (ts=%d; deadline=%d): %w", ts.Height(), di, err)
	}
	params.ChainCommitEpoch = commEpoch
	params.ChainCommitRand = commRand

	elapsed := time.Since(tsStart)
	log.Infow("submitting window PoSt", "deadline", di.Index, "elapsed", elapsed)

	return params, nil
}

func (s *WindowPoStScheduler) sectorsForProof(ctx context.Context, goodSectors, allSectors bitfield.BitField, ts *types.TipSet) ([]proof.SectorInfo, error) {
	sset, err := s.api.StateMinerSectors(ctx, s.actor, &goodSectors, false, ts.Key())
	if err != nil {
		return nil, err
	}

	if len(sset) == 0 {
		return nil, nil
	}

	substitute := proof.SectorInfo{
		SectorNumber: sset[0].ID,
		SealedCID:    sset[0].Info.SealedCID,
		SealProof:    sset[0].Info.SealProof,
	}

	sectorByID := make(map[uint64]proof.SectorInfo, len(sset))
	for _, sector := range sset {
		sectorByID[uint64(sector.ID)] = proof.SectorInfo{
			SectorNumber: sector.ID,
			SealedCID:    sector.Info.SealedCID,
			SealProof:    sector.Info.SealProof,
		}
	}

	proofSectors := make([]proof.SectorInfo, 0, len(sset))
	if err := allSectors.ForEach(func(sectorNo uint64) error {
		if info, found := sectorByID[sectorNo]; found {
			proofSectors = append(proofSectors, info)
		} else {
			proofSectors = append(proofSectors, substitute)
		}
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("iterating partition sector bitmap: %w", err)
	}

	return proofSectors, nil
}

func (s *WindowPoStScheduler) submitPost(ctx context.Context, proof *miner.SubmitWindowedPoStParams) (*types.SignedMessage, error) {
	ctx, span := trace.StartSpan(ctx, "storage.commitPost")
	defer span.End()

	var sm *types.SignedMessage

	enc, aerr := actors.SerializeParams(proof)
	if aerr != nil {
		return nil, xerrors.Errorf("could not serialize submit post parameters: %w", aerr)
	}

	msg := &types.Message{
		To:     s.actor,
		From:   s.worker,
		Method: builtin.MethodsMiner.SubmitWindowedPoSt,
		Params: enc,
		Value:  types.NewInt(0),
	}
	spec := &api.MessageSendSpec{MaxFee: abi.TokenAmount(s.feeCfg.MaxWindowPoStGasFee)}
	s.setSender(ctx, msg, spec)

	if s.noSubmit {
		log.Info("noSubmit for SubmitWindowedPoSt")
		return nil, errors.New("no submit is open")
	}

	// TODO: consider maybe caring about the output
	sm, err := s.api.MpoolPushMessage(ctx, msg, spec)

	if err != nil {
		return nil, xerrors.Errorf("pushing message to mpool: %w", err)
	}

	log.Infof("Submitting window post %d: %s", proof.Deadline, sm.Cid())

	go func() {
		rec, err := s.api.StateWaitMsg(context.TODO(), sm.Cid(), build.MessageConfidence)
		if err != nil {
			log.Error(errors.As(err, proof.Deadline))
			return
		}

		if rec.Receipt.ExitCode == 0 {
			log.Infof("Submitting window post %d, %s success.", proof.Deadline, sm.Cid())
			return
		}

		log.Errorf("Submitting window post %d, %s failed: exit %d", proof.Deadline, sm.Cid(), rec.Receipt.ExitCode)
	}()

	return sm, nil
}

func (s *WindowPoStScheduler) setSender(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec) {
	mi, err := s.api.StateMinerInfo(ctx, s.actor, types.EmptyTSK)
	if err != nil {
		log.Errorw("error getting miner info", "error", err)

		// better than just failing
		msg.From = s.worker
		return
	}

	gm, err := s.api.GasEstimateMessageGas(ctx, msg, spec, types.EmptyTSK)
	if err != nil {
		log.Errorw("estimating gas", "error", err)
		msg.From = s.worker
		return
	}
	*msg = *gm

	minFunds := big.Add(msg.RequiredFunds(), msg.Value)

	pa, err := AddressFor(ctx, s.api, mi, PoStAddr, minFunds)
	if err != nil {
		log.Errorw("error selecting address for post", "error", err)
		msg.From = s.worker
		return
	}

	msg.From = pa
}
