package database

import (
	"context"
	"fmt"
	"github.com/gwaylib/errors"
	"strconv"
	"sync"

	"huangdong2012/filecoin-monitor/trace/spans"
)

// 不使用ffiwrapper.WorkerTaskType枚举 避免循环引用
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

	sectorProving = 200
)

var (
	ssm = &SectorSpans{
		spans: &sync.Map{},
	}
)

type SectorSpans struct {
	spans *sync.Map
}

func (s *SectorSpans) removeSpan(id, step string) {
	key := fmt.Sprintf("%v-%v", id, step)
	s.spans.Delete(key)
}

func (s *SectorSpans) getSpan(step string, info *SectorInfo, wInfo *WorkerInfo) *spans.SectorSpan {
	key := fmt.Sprintf("%v-%v", info.ID, step)
	if val, ok := s.spans.Load(key); ok {
		return val.(*spans.SectorSpan)
	}

	_, span := spans.NewSectorSpan(context.Background())
	span.SetID(info.ID)
	span.SetMinerID(info.MinerId)
	span.SetStep(step)
	span.SetSealedStorageID(strconv.FormatInt(info.StorageSealed, 10))
	span.SetUnSealedStorageID(strconv.FormatInt(info.StorageUnsealed, 10))
	span.SetWorkNo(info.WorkerId)
	if wInfo != nil {
		span.SetWorkIP(wInfo.Ip)
	}
	s.spans.Store(key, span)

	return span
}

func (s *SectorSpans) getStep(state int) string {
	switch state {
	case workerPledge, workerPledgeDone:
		return "Pledge"
	case workerPreCommit1, workerPreCommit1Done:
		return "PreCommit1"
	case workerPreCommit2, workerPreCommit2Done:
		return "PreCommit2"
	case workerCommit, workerCommitDone:
		return "Commit"
	case workerUnseal, workerUnsealDone:
		return "Unseal"
	case workerFinalize:
		return "Finalize"
	case sectorProving:
		return "Proving"
	}
	return ""
}

func (s *SectorSpans) isStepDone(state int) bool {
	return state == workerPledgeDone ||
		state == workerPreCommit1Done ||
		state == workerPreCommit2Done ||
		state == workerCommitDone ||
		state == workerUnsealDone ||
		state == workerFinalize ||
		state == sectorProving
}

func (s *SectorSpans) OnSectorStateChange(info *SectorInfo, wInfo *WorkerInfo, wid, msg string, state int) {
	if info == nil {
		return
	}

	if step := s.getStep(state); len(step) > 0 {
		if span := s.getSpan(step, info, wInfo); s.isStepDone(state) {
			span.Finish(nil)
			s.removeSpan(info.ID, step)
		} else {
			span.Starting(msg)
		}
	} else if state == SECTOR_STATE_FAILED {
		if step = s.getStep(info.State); len(step) > 0 && !s.isStepDone(info.State) {
			span := s.getSpan(step, info, wInfo)
			span.Finish(errors.New(msg))
			s.removeSpan(info.ID, step)
		}
	}
}
