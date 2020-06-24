package storage

import (
	"context"
	"strconv"

	"github.com/gwaylib/errors"
)

func (m *Miner) Testing(ctx context.Context, fnName string, args []string) error {
	switch fnName {
	case "checkWindowPoSt":
		if len(args) != 2 {
			return errors.New("error argument input")
		}
		index, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return errors.As(err, args)
		}

		m.fps.checkWindowPoSt(ctx, index, args[1] == "true")
	}
	return nil
}

func (s *WindowPoStScheduler) checkWindowPoSt(ctx context.Context, index uint64, submit bool) {
	log.Info("DEBUG:checkWindowPoStPost")
	new, err := s.api.ChainHead(ctx)
	if err != nil {
		panic(err)
	}

	//new, err := s.api.ChainGetTipSetByHeight(ctx, 51840, types.EmptyTSK)
	//if err != nil {
	//	panic(err)
	//}
	deadline, err := s.api.StateMinerProvingDeadline(ctx, s.actor, new.Key())
	if err != nil {
		panic(err)
	}
	ts := new

	log.Infof("DEBUG:tipset:%d,%d,%+v", new.Height(), ts.Height(), deadline)
	deadline.Index = index

	proof, err := s.runPost(ctx, *deadline, ts)
	switch err {
	case errNoPartitions:
		log.Info("NoPartitions")
		return
	case nil:
		// no commit
		log.Infof("submit window post:%t", submit)
		if submit {
			if err := s.submitPost(ctx, proof); err != nil {
				log.Errorf("submitPost failed: %+v", err)
				return
			}
		}

		return
	default:
		log.Errorf("runPost failed: %+v", err)
		return
	}
}
