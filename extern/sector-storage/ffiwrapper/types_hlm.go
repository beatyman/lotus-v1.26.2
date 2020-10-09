package ffiwrapper

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

func SectorName(sid abi.SectorID) string {
	return fmt.Sprintf("s-t0%d-%d", sid.Miner, sid.Number)
}
func SectorID(sid string) abi.SectorID {
	id, err := ParseSectorID(sid)
	if err != nil {
		panic(err)
	}
	return id
}
func ParseSectorID(sid string) (abi.SectorID, error) {
	ids := strings.Split(sid[4:], "-")
	if len(ids) != 2 {
		return abi.SectorID{}, errors.New("error id format").As(sid)
	}
	minerId, err := strconv.ParseUint(ids[0], 10, 64)
	if err != nil {
		return abi.SectorID{}, errors.New("error id format").As(sid)
	}
	sectorId, err := strconv.ParseUint(ids[1], 10, 64)
	if err != nil {
		return abi.SectorID{}, errors.New("error id format").As(sid)
	}
	return abi.SectorID{
		Miner:  abi.ActorID(minerId),
		Number: abi.SectorNumber(sectorId),
	}, nil
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

type SectorCids struct {
	Unsealed string // encoded cid.Cid
	Sealed   string // encoded cid.Cid
}

func (s *SectorCids) Decode() (*storage.SectorCids, error) {
	unsealedCid, err := cid.Decode(s.Unsealed)
	if err != nil {
		return nil, errors.As(err, *s)
	}
	sealedCid, err := cid.Decode(s.Sealed)
	if err != nil {
		return nil, errors.As(err, *s)
	}
	return &storage.SectorCids{
		Unsealed: unsealedCid,
		Sealed:   sealedCid,
	}, nil
}

type PieceInfo struct {
	Size     abi.PaddedPieceSize
	PieceCID string
}

func (p *PieceInfo) Decode() (*abi.PieceInfo, error) {
	pCID, err := cid.Decode(p.PieceCID)
	if err != nil {
		return nil, errors.As(err)
	}
	return &abi.PieceInfo{
		Size:     p.Size,
		PieceCID: pCID,
	}, nil
}

func EncodePieceInfo(in []abi.PieceInfo) []PieceInfo {
	out := []PieceInfo{}
	for _, p := range in {
		out = append(out, PieceInfo{
			Size:     p.Size,
			PieceCID: p.PieceCID.String(),
		})
	}
	return out
}
func DecodePieceInfo(in []PieceInfo) ([]abi.PieceInfo, error) {
	out := []abi.PieceInfo{}
	for i, p := range in {
		tmpCID, err := cid.Decode(p.PieceCID)
		if err != nil {
			return nil, errors.As(err, i, in)
		}
		out = append(out, abi.PieceInfo{
			Size:     p.Size,
			PieceCID: tmpCID,
		})
	}
	return out, nil
}

type WorkerTaskType int

const (
	WorkerAddPiece       WorkerTaskType = 0
	WorkerAddPieceDone                  = 1
	WorkerPreCommit1                    = 10
	WorkerPreCommit1Done                = 11
	WorkerPreCommit2                    = 20
	WorkerPreCommit2Done                = 21
	WorkerCommit1                       = 30
	WorkerCommit1Done                   = 31
	WorkerCommit2                       = 40
	WorkerCommit2Done                   = 41
	WorkerFinalize                      = 50

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
	ParallelAddPiece   int
	ParallelPrecommit1 int
	ParallelPrecommit2 int
	ParallelCommit1    int
	ParallelCommit2    int

	Commit2Srv bool // need ParallelCommit2 > 0
	WdPoStSrv  bool
	WnPoStSrv  bool
}

type WorkerTask struct {
	Type WorkerTaskType
	// TaskID uint64 // using SecotrID instead

	SectorID      abi.SectorID
	WorkerID      string
	SectorStorage database.SectorStorage

	// addpiece
	PieceSizes []abi.UnpaddedPieceSize

	// preCommit1
	SealTicket abi.SealRandomness // commit1 is need too.
	Pieces     []PieceInfo        // commit1 is need too.

	// preCommit2
	PreCommit1Out storage.PreCommit1Out

	// commit1
	SealSeed abi.InteractiveSealRandomness
	Cids     SectorCids

	// commit2
	Commit1Out storage.Commit1Out

	// winning PoSt
	SectorInfo []proof.SectorInfo
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

func (w *WorkerTask) GetSectorID() string {
	return SectorName(w.SectorID)
}

type workerCall struct {
	task WorkerTask
	ret  chan SealRes
}
type WorkerStats struct {
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

	AddPieceWait   int
	PreCommit1Wait int
	PreCommit2Wait int
	Commit1Wait    int
	Commit2Wait    int
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
	tasks := []string{}
	for _, s := range w.SectorOn {
		tasks = append(tasks, fmt.Sprintf("%s:%d", s.ID, s.State))
	}
	return fmt.Sprintf(
		"id:%s,disable:%t,srv:%t,ip:%s,busy:%s,sectors:%+v",
		w.ID, w.Disable, w.Srv, w.IP, w.BusyOn, tasks,
	)
}

type remote struct {
	lock    sync.Mutex
	cfg     WorkerCfg
	release func()

	// recieve owner task
	precommit1Wait int32
	precommit1Chan chan workerCall
	precommit2Wait int32
	precommit2Chan chan workerCall
	commit1Chan    chan workerCall
	commit2Chan    chan workerCall
	finalizeChan   chan workerCall

	sealTasks   chan<- WorkerTask
	busyOnTasks map[string]WorkerTask // length equals WorkerCfg.MaxCacheNum, key is sector id.
	disable     bool                  // disable for new sector task
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
	case WorkerCommit1, WorkerFinalize:
		return false
	}

	busyAddPieceNum := 0
	busyPrecommit1Num := 0
	busyPrecommit2Num := 0
	busyCommit1Num := 0
	busyCommit2Num := 0
	busyWinningPoSt := 0
	busyWindowPoSt := 0
	sumWorkingTask := 0
	for _, val := range r.busyOnTasks {
		if val.Type%10 == 0 {
			sumWorkingTask++
		}
		switch val.Type {
		case WorkerAddPiece:
			busyAddPieceNum++
		case WorkerPreCommit1:
			busyPrecommit1Num++
		case WorkerPreCommit2:
			busyPrecommit2Num++
		case WorkerCommit1:
			busyCommit1Num++
		case WorkerCommit2:
			busyCommit2Num++
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
	case WorkerAddPiece:
		// mutex cpu for addpiece and precommit1
		return busyAddPieceNum >= r.cfg.ParallelAddPiece || (busyPrecommit1Num > 0)
	case WorkerPreCommit1:
		return busyPrecommit1Num >= r.cfg.ParallelPrecommit1 || (busyAddPieceNum > 0)
	case WorkerPreCommit2:
		// mutex gpu for precommit2, commit2.
		return busyPrecommit2Num >= r.cfg.ParallelPrecommit2 || (r.cfg.Commit2Srv && busyCommit2Num > 0)
	case WorkerCommit2:
		// ulimit to call commit2 service.
		if r.cfg.ParallelCommit2 == 0 {
			return false
		}
		return busyCommit2Num >= r.cfg.ParallelCommit2 || (busyPrecommit2Num > 0)
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
		if restore && wTask.State == int(WorkerAddPiece) {
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
				r.busyOnTasks[wTask.ID] = WorkerTask{
					Type:     WorkerTaskType(wTask.State + 1),
					SectorID: abi.SectorID(SectorID(wTask.ID)),
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
	PreCommit2Out SectorCids
	Commit1Out    storage.Commit1Out
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
