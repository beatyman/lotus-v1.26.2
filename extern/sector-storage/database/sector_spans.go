package database

import (
	"context"
	"fmt"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"huangdong2012/filecoin-monitor/trace/spans"
	"strconv"
	"sync"
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
	case int(ffiwrapper.WorkerPledge):
		span := s.getSpan(info.ID, "Pledge", info)
		span.Starting(msg)
	case int(ffiwrapper.WorkerPledgeDone):
		span := s.getSpan(info.ID, "Pledge", info)
		span.Finish(nil)
	case int(ffiwrapper.WorkerPreCommit1):

	case int(ffiwrapper.WorkerPreCommit1Done):
		span := s.getSpan(info.ID, "PreCommit1", info)
		span.Finish(nil)
	case int(ffiwrapper.WorkerPreCommit2):

	case int(ffiwrapper.WorkerPreCommit2Done):
		span := s.getSpan(info.ID, "PreCommit2", info)
		span.Finish(nil)
	case int(ffiwrapper.WorkerCommit):

	case int(ffiwrapper.WorkerCommitDone):
		span := s.getSpan(info.ID, "Commit", info)
		span.Finish(nil)
	case int(ffiwrapper.WorkerFinalize):

	case int(ffiwrapper.WorkerUnseal):

	case int(ffiwrapper.WorkerUnsealDone):
		span := s.getSpan(info.ID, "Unseal", info)
		span.Finish(nil)
	}
}
