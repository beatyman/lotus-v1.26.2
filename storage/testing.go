package storage

import (
	"context"
	"strconv"

	"github.com/filecoin-project/go-state-types/abi"
	fapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/gwaylib/errors"
)

func (m *Miner) GetWdPoStLog(ctx context.Context, index uint64) ([]fapi.WdPoStLog, error) {
	return m.fps.GetLog(index), nil
}

func (m *Miner) Testing(ctx context.Context, fnName string, args []string) error {
	switch fnName {
	case "checkWindowPoSt":
		if len(args) != 2 {
			return errors.New("error argument input")
		}
		height, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return errors.As(err, args)
		}
		m.fps.checkWindowPoSt(ctx, abi.ChainEpoch(height), args[1] == "true")
	}
	return nil
}

func (s *WindowPoStScheduler) checkWindowPoSt(ctx context.Context, height abi.ChainEpoch, submit bool) {
	log.Info("DEBUG:checkWindowPoStPost")

	var new *types.TipSet
	if height > 0 {
		ts, err := s.api.ChainGetTipSetByHeight(ctx, height, types.EmptyTSK)
		if err != nil {
			panic(err)
		}
		new = ts
	} else {
		ts, err := s.api.ChainHead(ctx)
		if err != nil {
			panic(err)
		}
		new = ts
	}

	deadline, err := s.api.StateMinerProvingDeadline(ctx, s.actor, new.Key())
	if err != nil {
		panic(err)
	}
	ts := new

	log.Infof("DEBUG:tipset:%d,%d,%+v", new.Height(), ts.Height(), deadline)
	// deadline.Index = index
	posts, err := s.runPost(ctx, *deadline, ts)
	if err != nil {
		log.Errorf("runPost failed: %+v", err)
		return
	}
	// no commit
	log.Infof("submit window post:%t", submit)
	if !submit {
		return
	}
	if s.runSubmitPoST(ctx, new, deadline, posts); err != nil {
		log.Errorf("submit window post failed: %+v", err)
	}
	return
}