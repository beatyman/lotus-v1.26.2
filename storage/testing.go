package storage

import (
	"context"

	"github.com/gwaylib/errors"
)

func (m *Miner) Testing(ctx context.Context, fnName string, args []string) error {
	return errors.New("Not implements").As(fnName)
	//switch fnName {
	//case "checkWindowPoSt":
	//	if len(args) != 2 {
	//		return errors.New("error argument input")
	//	}
	//	height, err := strconv.ParseUint(args[0], 10, 64)
	//	if err != nil {
	//		return errors.As(err, args)
	//	}
	//	m.fps.checkWindowPoSt(ctx, abi.ChainEpoch(height), args[1] == "true")
	//}
	//return nil
}

//func (s *WindowPoStScheduler) checkWindowPoSt(ctx context.Context, height abi.ChainEpoch, submit bool) {
//	log.Info("DEBUG:checkWindowPoStPost")
//
//	var new *types.TipSet
//	if height > 0 {
//		ts, err := s.api.ChainGetTipSetByHeight(ctx, height, types.EmptyTSK)
//		if err != nil {
//			panic(err)
//		}
//		new = ts
//	} else {
//		ts, err := s.api.ChainHead(ctx)
//		if err != nil {
//			panic(err)
//		}
//		new = ts
//	}
//
//	deadline, err := s.api.StateMinerProvingDeadline(ctx, s.actor, new.Key())
//	if err != nil {
//		panic(err)
//	}
//	ts := new
//
//	log.Infof("DEBUG:tipset:%d,%d,%+v", new.Height(), ts.Height(), deadline)
//	// deadline.Index = index
//	posts, err := s.runPost(ctx, submit, *deadline, ts)
//	if err != nil {
//		log.Errorf("runPost failed: %+v", err)
//		return
//	}
//	// no commit
//	log.Infof("submit window post:%t", submit)
//	if !submit {
//		return
//	}
//	for i := range posts {
//		post := &posts[i]
//		_, err := s.submitPost(ctx, submit, post)
//		if err != nil {
//			log.Errorf("submit window post failed: %+v", err)
//		}
//	}
//	return
//}
