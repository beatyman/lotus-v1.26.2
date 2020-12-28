package ffiwrapper

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
	"github.com/gwaylib/errors"
)

func sectorName(sid abi.SectorID) string {
	return storage.SectorName(sid)
}

func sectorID(sid string) abi.SectorID {
	id, err := storage.ParseSectorID(sid)
	if err != nil {
		panic(err)
	}
	return id
}

const (
	DataCache    = "cache"
	DataStaging  = "staging"
	DataSealed   = "sealed"
	DataUnsealed = "unsealed"
)

type RemoteCfg struct {
	SealSector  bool
	WindowPoSt  int // 0, close remote function, >0, using number of thread to work at the same time.
	WinningPoSt int // 0, close remote function, >0, using number of thread to work at the same time.
}

type WorkerTaskType int

const (
	WorkerPledge         WorkerTaskType = 0
	WorkerPledgeDone                    = 1
	WorkerPreCommit1                    = 10
	WorkerPreCommit1Done                = 11
	WorkerPreCommit2                    = 20
	WorkerPreCommit2Done                = 21
	WorkerCommit                        = 40
	WorkerCommitDone                    = 41
	WorkerFinalize                      = 50
	WorkerUnseal                        = 60
	WorkerUnsealDone                    = 61

	WorkerTransfer = 101

	WorkerWindowPoSt  = 1000
	WorkerWinningPoSt = 1010
)

var (
	ErrWorkerExit   = errors.New("Worker Exit")
	ErrTaskNotFound = errors.New("Task not found")
	ErrTaskDone     = errors.New("Task Done")
)

type WorkerCfg struct {
	ID string // worker id, default is empty for same worker.
	IP string // worker current ip

	//  the seal data
	SvcUri string

	// function switch
	MaxTaskNum         int // need more than 0
	CacheMode          int // 0, transfer mode; 1, share mode.
	TransferBuffer     int // 0~n, do next task when transfering and transfer cache on
	ParallelPledge     int
	ParallelPrecommit1 int // unseal is shared with this parallel
	ParallelPrecommit2 int
	ParallelCommit     int

	Commit2Srv bool // need ParallelCommit2 > 0
	WdPoStSrv  bool
	WnPoStSrv  bool
}

type WorkerTask struct {
	Type WorkerTaskType
	// TaskID uint64 // using SecotrID instead

	ProofType     abi.RegisteredSealProof
	SectorID      abi.SectorID
	WorkerID      string
	SectorStorage database.SectorStorage

	// addpiece
	ExistingPieceSizes []abi.UnpaddedPieceSize
	ExtSizes           []abi.UnpaddedPieceSize // size ...abi.UnpaddedPieceSize

	// unseal
	UnsealOffset   storiface.UnpaddedByteIndex
	UnsealSize     abi.UnpaddedPieceSize
	SealRandomness abi.SealRandomness
	Commd          cid.Cid

	// preCommit1
	SealTicket abi.SealRandomness // commit1 is need too.
	Pieces     []abi.PieceInfo

	// preCommit2
	PreCommit1Out storage.PreCommit1Out

	// commit1
	SealSeed abi.InteractiveSealRandomness
	Cids     storage.SectorCids

	// commit2
	Commit1Out storage.Commit1Out

	// winning PoSt
	SectorInfo []storage.ProofSectorInfo
	// using for wdpost, winpost, unseal
	Randomness abi.PoStRandomness

	// window PoSt
	// MinerID is in SectorID
	// SectorInfo same as winning PoSt
	// Randomness same as winning PoSt

}

func ParseTaskKey(key string) (string, int, error) {
	arr := strings.Split(key, "_")
	if len(arr) != 2 {
		return "", 0, errors.New("not a key format").As(key)
	}
	stage, err := strconv.Atoi(arr[1])
	if err != nil {
		return "", 0, errors.New("not a key format").As(key)
	}
	return arr[0], stage, nil
}

func (w *WorkerTask) Key() string {
	return fmt.Sprintf("s-t0%d-%d_%d", w.SectorID.Miner, w.SectorID.Number, w.Type)
}

func (w *WorkerTask) SectorName() string {
	return storage.SectorName(w.SectorID)
}

type workerCall struct {
	task WorkerTask
	ret  chan SealRes
}
type WorkerStats struct {
	PauseSeal      int32
	WorkerOnlines  int
	WorkerOfflines int
	WorkerDisabled int

	SealWorkerTotal  int
	SealWorkerUsing  int
	SealWorkerLocked int

	Commit2SrvTotal int
	Commit2SrvUsed  int

	WnPoStSrvTotal int
	WnPoStSrvUsed  int
	WdPoStSrvTotal int
	WdPoStSrvUsed  int

	PledgeWait     int
	PreCommit1Wait int
	PreCommit2Wait int
	CommitWait     int
	UnsealWait     int
	FinalizeWait   int
	WPostWait      int
	PostWait       int
}
type WorkerRemoteStats struct {
	ID       string
	IP       string
	Disable  bool
	Online   bool
	Srv      bool
	BusyOn   string
	SectorOn database.WorkingSectors
}

func (w *WorkerRemoteStats) String() string {
	history := []string{}
	for _, info := range w.SectorOn {
		if info.State >= 100 {
			history = append(history, fmt.Sprintf("%s_%d", info.ID, info.State))
		}
	}
	return fmt.Sprintf(
		"id:%s,disable:%t,online:%t,srv:%t,ip:%s,busy:%s,cache(%d):%+v",
		w.ID, w.Disable, w.Online, w.Srv, w.IP, w.BusyOn, len(history), history,
	)
}

type remote struct {
	ctx     context.Context
	lock    sync.Mutex
	cfg     WorkerCfg
	release func()

	// recieve owner task
	precommit1Wait int32
	precommit1Chan chan workerCall
	precommit2Wait int32
	precommit2Chan chan workerCall
	commitChan     chan workerCall
	finalizeChan   chan workerCall
	unsealChan     chan workerCall

	sealTasks   chan<- WorkerTask
	busyOnTasks map[string]WorkerTask // length equals WorkerCfg.MaxCacheNum, key is sector id.
	disable     bool                  // disable for new sector task

	srvConn int64
}

func (r *remote) busyOn(sid string) bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	_, ok := r.busyOnTasks[sid]
	return ok
}

// for control the disk space
func (r *remote) fullTask() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return len(r.busyOnTasks) >= r.cfg.MaxTaskNum
}

// for control the memory
func (r *remote) LimitParallel(typ WorkerTaskType, isSrvCalled bool) bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.limitParallel(typ, isSrvCalled)
}

func (r *remote) limitParallel(typ WorkerTaskType, isSrvCalled bool) bool {

	// no limit list
	switch typ {
	case WorkerFinalize:
		return false
	}

	busyPledgeNum := 0
	busyPrecommit1Num := 0
	busyPrecommit2Num := 0
	busyCommitNum := 0
	busyUnsealNum := 0
	busyWinningPoSt := 0
	busyWindowPoSt := 0
	sumWorkingTask := 0
	for _, val := range r.busyOnTasks {
		if val.Type%10 == 0 {
			sumWorkingTask++
		}
		switch val.Type {
		case WorkerPledge:
			busyPledgeNum++
		case WorkerPreCommit1:
			busyPrecommit1Num++
		case WorkerPreCommit2:
			busyPrecommit2Num++
		case WorkerCommit:
			busyCommitNum++
		case WorkerUnseal:
			busyUnsealNum++
		case WorkerWinningPoSt:
			busyWinningPoSt++
		case WorkerWindowPoSt:
			busyWindowPoSt++
		}
	}
	// in full working
	if sumWorkingTask >= r.cfg.MaxTaskNum {
		return true
	}
	if isSrvCalled {
		// mutex to any other task.
		return sumWorkingTask > 0
	}

	switch typ {
	// mutex cpu for addpiece and precommit1
	case WorkerPledge:
		return busyPledgeNum >= r.cfg.ParallelPledge || len(r.busyOnTasks) >= r.cfg.MaxTaskNum || busyPrecommit1Num+busyUnsealNum >= r.cfg.ParallelPrecommit1
	case WorkerPreCommit1, WorkerUnseal: // unseal is shared with the parallel-precommit1
		return busyPrecommit1Num+busyUnsealNum >= r.cfg.ParallelPrecommit1 || (busyPledgeNum > 0)

	// mutex gpu for precommit2, commit2.
	case WorkerPreCommit2:
		return busyPrecommit2Num >= r.cfg.ParallelPrecommit2 || (r.cfg.Commit2Srv && busyCommitNum > 0)
	case WorkerCommit:
		// ulimit to call commit2 service.
		if r.cfg.ParallelCommit == 0 {
			return false
		}
		return busyCommitNum >= r.cfg.ParallelCommit || (busyPrecommit2Num > 0)
	}
	// default is false to pass the logic.
	return false
}

func (r *remote) UpdateTask(sid string, state int) bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	task, ok := r.busyOnTasks[sid]
	if !ok {
		return false
	}
	task.Type = WorkerTaskType(state)
	r.busyOnTasks[sid] = task
	return ok

}
func (r *remote) freeTask(sid string) bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	_, ok := r.busyOnTasks[sid]
	delete(r.busyOnTasks, sid)
	return ok
}
func (r *remote) checkCache(restore bool, ignore []string) (full bool, err error) {
	// restore from database
	history, err := database.GetWorking(r.cfg.ID)
	if err != nil {
		// the error not nil
		if !errors.ErrNoData.Equal(err) {
			return false, errors.As(err, r.cfg.ID)
		}
		// no working data in cache
		return false, nil
	}
	for _, wTask := range history {
		for _, ign := range ignore {
			if ign == wTask.ID {
				return false, nil
			}
		}
	}
	for _, wTask := range history {
		if restore && wTask.State < 0 {
			log.Infof("Got free worker:%s, but has found history addpiece, will release:%+v", r.cfg.ID, wTask)
			if err := database.UpdateSectorState(
				wTask.ID, wTask.WorkerId,
				"release addpiece by history",
				database.SECTOR_STATE_FAILED,
			); err != nil {
				return false, errors.As(err, r.cfg.ID)
			}
			continue
		}
		if wTask.State <= WorkerFinalize {
			// maxTaskNum has changed to less, so only load a part
			if wTask.State < WorkerFinalize && len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
				break
			}
			r.lock.Lock()
			_, ok := r.busyOnTasks[wTask.ID]
			if !ok {
				// if no data in busy, restore from db, and waitting retry from storage-fsm
				stateOff := 0
				if (wTask.State % 10) == 0 {
					stateOff = 1
				}
				r.busyOnTasks[wTask.ID] = WorkerTask{
					Type:     WorkerTaskType(wTask.State + stateOff),
					SectorID: abi.SectorID(sectorID(wTask.ID)),
					WorkerID: wTask.WorkerId,
					// others not implement yet, it should be update by doSealTask()
				}
			}
			r.lock.Unlock()
		}
	}
	return history.IsFullWork(r.cfg.MaxTaskNum, r.cfg.TransferBuffer), nil
}

type SealRes struct {
	Type   WorkerTaskType
	TaskID string

	WorkerCfg WorkerCfg

	Err   string
	GoErr error `json:"-"`

	// TODO: Serial
	Pieces        []abi.PieceInfo
	PreCommit1Out storage.PreCommit1Out
	PreCommit2Out storage.SectorCids
	Commit2Out    storage.Proof

	WinningPoStProofOut []proof.PoStProof

	WindowPoStProofOut   []proof.PoStProof
	WindowPoStIgnSectors []abi.SectorID
}

func (s *SealRes) SectorID() string {
	arr := strings.Split(s.TaskID, "_")
	if len(arr) >= 1 {
		return arr[0]
	}
	return ""
}
