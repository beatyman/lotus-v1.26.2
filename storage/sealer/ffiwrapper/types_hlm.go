package ffiwrapper

import (
	"context"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/gwaylib/errors"
)

func sectorName(sid abi.SectorID) string {
	return storiface.SectorName(sid)
}

func sectorID(sid string) abi.SectorID {
	id, err := storiface.ParseSectorID(sid)
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
	SealSector                  bool
	WindowPoSt                  int // 0, close remote function, >0, using number of thread to work at the same time.
	WinningPoSt                 int // 0, close remote function, >0, using number of thread to work at the same time.
	EnableForceRemoteWindowPoSt bool
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
	WorkerFinalizeDone                  = 51
	WorkerUnseal                        = 60
	WorkerUnsealDone                    = 61

	WorkerTransfer = 101

	WorkerWindowPoSt  = 1000
	WorkerWinningPoSt = 1010
)

func (wt WorkerTaskType) IsSealStage() bool {
	switch wt {
	case WorkerPledge, WorkerPledgeDone,
		WorkerPreCommit1, WorkerPreCommit1Done,
		WorkerPreCommit2, WorkerPreCommit2Done,
		WorkerCommit, WorkerCommitDone,
		WorkerFinalize, WorkerFinalizeDone,
		WorkerUnseal, WorkerUnsealDone,
		WorkerWindowPoSt, WorkerWinningPoSt:
		return true
	}
	return false

}

func (wt WorkerTaskType) Stage() string {
	switch wt {
	case WorkerPledge, WorkerPledgeDone:
		return database.SEAL_STAGE_AP
	case WorkerPreCommit1, WorkerPreCommit1Done:
		return database.SEAL_STAGE_P1
	case WorkerPreCommit2, WorkerPreCommit2Done:
		return database.SEAL_STAGE_P2
	case WorkerCommit, WorkerCommitDone:
		return database.SEAL_STAGE_COMMIT
	case WorkerFinalize, WorkerFinalizeDone:
		return database.SEAL_STAGE_FZ
	case WorkerUnseal, WorkerUnsealDone:
		return database.SEAL_STAGE_US
	case WorkerWindowPoSt:
		return database.SEAL_STAGE_WD
	case WorkerWinningPoSt:
		return database.SEAL_STAGE_WN
	}
	return fmt.Sprintf("unknow_%d", wt)
}

var (
	ErrWorkerExit   = errors.New("Worker Exit")
	ErrTaskNotFound = errors.New("Task not found")
	ErrTaskDone     = errors.New("Task Done")
)

type Commit2Worker struct {
	WorkerId string
	Url      string
	Proof    string //hex.EncodeToString(storage.Proof)
}

type Commit2Result struct {
	WorkerId string
	TaskKey  string
	Sid      string

	Err        string
	Proof      string //hex.EncodeToString(storage.Proof)
	FinishTime time.Time
	Snap       bool //true: snap升级的任务 false:普通密封任务
}

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

	Cycle  string         //每次启动(或重启)后生成的uuid(Cycle+Retry 唯一标识一次连接)
	Retry  int            //worker断线重连次数 0:第一次连接（不是重连）
	Busy   []string       //p1 worker正在执行中的任务(miner重启->worker重连时需要: checkCache + Busy 恢复miner的busy状态)
	C2Sids []abi.SectorID //c2 worker正在执行的扇区id (c2断线重连后需要恢复busyOnTasks)
}

type WorkerQueueKind string

const (
	WorkerQueueKind_MinerReStart    WorkerQueueKind = "miner_restart"
	WorkerQueueKind_WorkerReStart   WorkerQueueKind = "worker_restart"
	WorkerQueueKind_WorkerReConnect WorkerQueueKind = "worker_reconnect"
)

type WorkerTask struct {
	TraceContext string //用于span传播
	Snap         bool   //true: snap升级的任务 false:普通密封任务

	Type WorkerTaskType
	// TaskID uint64 // using SecotrID instead
	PostProofType abi.RegisteredPoStProof
	ProofType     abi.RegisteredSealProof
	SectorID      abi.SectorID
	WorkerID      string
	SectorStorage database.SectorStorage

	// addpiece
	PieceSize          abi.UnpaddedPieceSize
	PieceData          storiface.PieceData

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
	PreCommit1Out storiface.PreCommit1Out

	// commit1
	SealSeed abi.InteractiveSealRandomness
	Cids     storiface.SectorCids

	// commit2
	Commit1Out storiface.Commit1Out

	// proveReplicaUpdate
	SectorKey   cid.Cid
	NewSealed   cid.Cid
	NewUnsealed cid.Cid

	// winning PoSt
	SectorInfo []storiface.ProofSectorInfo
	// using for wdpost, winpost, unseal
	Randomness abi.PoStRandomness

	// window PoSt
	// MinerID is in SectorID
	// SectorInfo same as winning PoSt
	// Randomness same as winning PoSt

	//扇区修复  0： 默认状态， 1.扇区修复标准状态， 2. 扇区修复执行指定二进制文件
	SectorRepairStatus int
	//是否存储unseal
	StoreUnseal bool
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
	return fmt.Sprintf("%s_%d", w.SectorName(), w.Type)
}

func (w *WorkerTask) SectorName() string {
	return storiface.SectorName(w.SectorID)
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
	Cfg      WorkerCfg
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
	lock    sync.RWMutex
	cfg     WorkerCfg
	release func()

	// recieve owner task
	pledgeWait     int32
	pledgeChan     chan workerCall
	precommit1Wait int32
	precommit1Chan chan workerCall
	precommit2Wait int32
	precommit2Chan chan workerCall
	commitChan     chan workerCall
	finalizeChan   chan workerCall
	unsealChan     chan workerCall

	sealTasks   chan WorkerTask
	busyOnTasks map[string]WorkerTask // length equals WorkerCfg.MaxCacheNum, key is sector id.
	disable     bool                  // disable for new sector task
	offline     int32                 //当前是否断线
	offlineRW   sync.RWMutex          //上线和下线的操作需要锁住

	srvConn int64

	dictBusy   map[string]string
	dictBusyRW sync.RWMutex
}

func (r *remote) isOfflineState() bool {
	return atomic.LoadInt32(&r.offline) > 0
}

func (r *remote) setOfflineState() {
	atomic.AddInt32(&r.offline, 1)
}

func (r *remote) clearOfflineState() {
	atomic.StoreInt32(&r.offline, 0)
}

func (r *remote) taskEnable(task WorkerTask) bool {
	switch task.Type {
	case WorkerCommit:
		return r.cfg.Commit2Srv
	case WorkerWinningPoSt:
		return r.cfg.WnPoStSrv
	case WorkerWindowPoSt:
		return r.cfg.WdPoStSrv
	}
	return true
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

func (r *remote) fakeFullTask() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	count := 0
	for _, task := range r.busyOnTasks {
		//只计算P1,P2任务
		if task.Type < WorkerPreCommit2Done {
			count++
		}
	}
	return count >= r.cfg.MaxTaskNum
}

func (r *remote) Idle() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	// TODO: consider the queue?
	num := 0
	for _, val := range r.busyOnTasks {
		if r.cfg.ID != val.WorkerID {
			continue
		}
		switch val.Type {
		case WorkerPledge:
			num++
		case WorkerPledgeDone:
			num++
		case WorkerPreCommit1:
			num++
		case WorkerPreCommit1Done:
			num++
		case WorkerPreCommit2:
			num++
		case WorkerUnseal:
			num++
		case WorkerUnsealDone:
			num++
		}
	}
	return r.cfg.MaxTaskNum - num
}

// for control the memory
func (r *remote) LimitParallel(typ WorkerTaskType, isSrvCalled bool) bool {
	if r.isOfflineState() {
		return true
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	return r.limitParallel(typ, isSrvCalled)
}

func (r *remote) limitParallel(typ WorkerTaskType, isSrvCalled bool) bool {

	// no limit list
	switch typ {
	case WorkerFinalize:
		// TODO: return r.cfg.Commit2Srv || r.cfg.WdPoStSrv || r.cfg.WnPoStSrv
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
	realWorking := 0
	for _, val := range r.busyOnTasks {
		if val.Type%10 == 0 {
			sumWorkingTask++
		}
		if val.Type <= WorkerPreCommit2 {
			realWorking++
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
	//log.Infof("workerIP: %v; real<=21: %v/%v ; AddPiece: %v/%v ; P1: %v/%v ; P2: %v/%v ;Commit: %v/%v", r.cfg.IP, realWorking, r.cfg.MaxTaskNum, busyPledgeNum, r.cfg.ParallelPledge, busyPrecommit1Num, r.cfg.ParallelPrecommit1, busyPrecommit2Num, r.cfg.ParallelPrecommit2, busyCommitNum, r.cfg.ParallelCommit)
	if isSrvCalled {
		// mutex to any other task.
		return sumWorkingTask > 0
	}

	switch typ {
	// mutex cpu for addpiece and precommit1
	case WorkerPledge:
		if realWorking >= r.cfg.MaxTaskNum {
			return true
		}
		return busyPledgeNum >= r.cfg.ParallelPledge || realWorking >= r.cfg.MaxTaskNum || busyPrecommit1Num+busyUnsealNum >= r.cfg.ParallelPrecommit1
	case WorkerPreCommit1, WorkerUnseal: // unseal is shared with the parallel-precommit1
		return busyPrecommit1Num+busyUnsealNum >= r.cfg.ParallelPrecommit1

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

// 根据worker上报的状态恢复因checkCache或ctx.Done()加1的任务
func (r *remote) checkBusy(wBusy []string) {
	log.Infow("check busy starting", "worker-id", r.cfg.ID, "worker-busy", wBusy)
	if len(wBusy) == 0 {
		return
	}

	dict := make(map[string]string)
	for _, sn := range wBusy {
		dict[sn] = sn
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	minerTaskKeys := make([]string, 0, 0)
	fixTaskKeys := make([]string, 0, 0)
	lostTaskKeys := make([]string, 0, 0)
	for sn, task := range r.busyOnTasks {
		minerTaskKeys = append(minerTaskKeys, task.Key())
		if _, ok := dict[sn]; ok && int(task.Type)%10 > 0 {
			task.Type -= 1
			r.busyOnTasks[sn] = task
			fixTaskKeys = append(fixTaskKeys, task.Key())
		}
	}
	for sn, _ := range dict {
		if _, ok := r.busyOnTasks[sn]; !ok {
			lostTaskKeys = append(lostTaskKeys, sn)
		}
	}
	log.Infow("check busy finish", "worker-id", r.cfg.ID, "miner-busy", minerTaskKeys, "fix-busy", fixTaskKeys, "lost-sector", lostTaskKeys)
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
				wTask.Snap,
			); err != nil {
				return false, errors.As(err, r.cfg.ID)
			}
			continue
		}
		if wTask.State <= WorkerFinalize {
			// maxTaskNum has changed to less, so only load a part
			/*
				if wTask.State < WorkerFinalize && len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
					break
				}
			*/
			r.lock.Lock()
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
			r.lock.Unlock()
		}
	}
	return r.fakeFullTask(), nil
}

type SealRes struct {
	Type   WorkerTaskType
	TaskID string

	WorkerCfg WorkerCfg

	Err   string
	GoErr error `json:"-"`

	// TODO: Serial
	Pieces        []abi.PieceInfo
	PreCommit1Out storiface.PreCommit1Out
	PreCommit2Out storiface.SectorCids
	Commit2Out    storiface.Proof
	//snap
	ReplicaUpdateOut      storiface.ReplicaUpdateOut
	ProveReplicaUpdateOut storiface.ReplicaUpdateProof

	WinningPoStProofOut []proof.PoStProof

	WindowPoStProofOut   []proof.PoStProof
	WindowPoStIgnSectors []abi.SectorID
	Piece                abi.PieceInfo
}

func (s *SealRes) SectorID() string {
	arr := strings.Split(s.TaskID, "_")
	if len(arr) >= 1 {
		return arr[0]
	}
	return ""
}