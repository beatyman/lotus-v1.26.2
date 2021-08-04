package database

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"huangdong2012/filecoin-monitor/trace/spans"
)

const (
	workerPledge         = 0
	workerPledgeDone     = 1
	workerPreCommit1     = 10
	workerPreCommit1Done = 11
	workerPreCommit2     = 20
	workerPreCommit2Done = 21
	workerCommit         = 40
	workerCommitDone     = 41
	workerFinalize       = 50
	workerUnseal         = 60
	workerUnsealDone     = 61
)

var (
	ssm = &SectorSpans{
		spans: &sync.Map{},
	}
)

type SectorSpans struct {
	spans *sync.Map
}

func (s *SectorSpans) getSpan(id string, step string, info *SectorInfo) *spans.SectorSpan {
	key := fmt.Sprintf("%v-%v", id, step)
	if val, ok := s.spans.Load(key); ok {
		return val.(*spans.SectorSpan)
	}

	_, span := spans.NewSectorSpan(context.Background())
	span.SetID(id)
	span.SetMinerID(info.MinerId)
	span.SetStep(step)
	span.SetSealedStorageID(strconv.FormatInt(info.StorageSealed, 10))
	span.SetUnSealedStorageID(strconv.FormatInt(info.StorageUnsealed, 10))
	s.spans.Store(key, span)

	return span
}

func (s *SectorSpans) OnSectorStateChange(info *SectorInfo, wid, msg string, state int) {
	if info == nil {
		return
	}

	switch state {
	case workerPledge:
		span := s.getSpan(info.ID, "Pledge", info)
		span.Starting(msg)
	case workerPledgeDone:
		span := s.getSpan(info.ID, "Pledge", info)
		span.Finish(nil)
	case workerPreCommit1:

	case workerPreCommit1Done:
		span := s.getSpan(info.ID, "PreCommit1", info)
		span.Finish(nil)
	case workerPreCommit2:

	case workerPreCommit2Done:
		span := s.getSpan(info.ID, "PreCommit2", info)
		span.Finish(nil)
	case workerCommit:

	case workerCommitDone:
		span := s.getSpan(info.ID, "Commit", info)
		span.Finish(nil)
	case workerFinalize:

	case workerUnseal:

	case workerUnsealDone:
		span := s.getSpan(info.ID, "Unseal", info)
		span.Finish(nil)
	}
}
