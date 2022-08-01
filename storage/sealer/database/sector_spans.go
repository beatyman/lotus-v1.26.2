package database

import (
	"context"
	"fmt"
	"github.com/gwaylib/errors"
	"go.opencensus.io/trace"
	"huangdong2012/filecoin-monitor/trace/spans"
	"strconv"
	"sync"
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
	WorkerCommitRun      = 42
	workerCommitDone     = 41
	workerFinalize       = 50
	workerFinalizeDone   = 51
	workerUnseal         = 60
	workerUnsealDone     = 61

	sectorSuccess = 200
	sectorError   = 500

	SectorProving     = 300
	SectorProvingDone = 301
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

func (s *SectorSpans) getSpan(step string, info *SectorInfo, wInfo *WorkerInfo, snap int) *spans.SectorSpan {
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
	span.AddAttributes(trace.BoolAttribute("snap", snap == 1))
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
	case workerCommit, WorkerCommitRun, workerCommitDone:
		return "Commit"
	case workerUnseal, workerUnsealDone:
		return "Unseal"
	case workerFinalize, workerFinalizeDone, sectorSuccess:
		return "Finalize"
	case SectorProving, SectorProvingDone:
		return "Proving"
	}
	return ""
}

func (s *SectorSpans) isStepRunning(state int) bool {
	return state == WorkerCommitRun
}

func (s *SectorSpans) isStepDone(state int) bool {
	return state == workerPledgeDone ||
		state == workerPreCommit1Done ||
		state == workerPreCommit2Done ||
		state == workerCommitDone ||
		state == workerUnsealDone ||
		state == workerFinalizeDone ||
		state == SectorProvingDone ||
		state == sectorSuccess ||
		state >= sectorError
}

func (s *SectorSpans) OnSectorStateChange(info *SectorInfo, wInfo *WorkerInfo, wid, msg string, state, snap int) {
	if info == nil {
		return
	}

	if step := s.getStep(state); len(step) > 0 {
		if span := s.getSpan(step, info, wInfo, snap); s.isStepDone(state) { //finish
			span.Finish(nil)
			s.removeSpan(info.ID, step)
		} else if s.isStepRunning(state) { //running
			if state == WorkerCommitRun {
				if wInfo != nil {
					span.AddAttributes(trace.StringAttribute("commit2_work_ip", wInfo.Ip))
					span.AddAttributes(trace.StringAttribute("commit2_work_no", wInfo.ID))
				}
				span.Process("commit2", msg)
			}
		} else { //starting
			span.Starting(msg)
		}
	} else if state >= SECTOR_STATE_FAILED {
		if step = s.getStep(info.State); len(step) > 0 && !s.isStepDone(info.State) {
			span := s.getSpan(step, info, wInfo, snap)
			span.Finish(errors.New(msg))
			s.removeSpan(info.ID, step)
		}
	}
}
