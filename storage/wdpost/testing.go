package wdpost

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

func (s *WindowPoStScheduler) CheckWindowPoSt(ctx context.Context, height abi.ChainEpoch, submit bool) {
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
	posts, err := s.runGeneratePoST(ctx, ts, deadline)
	if err != nil {
		log.Errorf("runPost failed: %+v", err)
		return
	}
	// no commit
	log.Infof("submit window post:%t", submit)
	if !submit {
		return
	}
	if err = s.runSubmitPoST(ctx, new, deadline, posts); err != nil {
		log.Errorf("submit window post failed: %+v", err)
	}
	return
}
