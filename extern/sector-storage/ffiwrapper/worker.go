package ffiwrapper

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

func (sb *Sealer) WorkerStats() WorkerStats {
	_remoteLk.RLock()
	defer _remoteLk.RUnlock()

	sealWorkerTotal := 0
	sealWorkerUsing := 0
	sealWorkerLocked := 0
	commit2SrvTotal := 0
	commit2SrvUsed := 0
	wnPoStSrvTotal := 0
	wnPoStSrvUsed := 0
	wdPoStSrvTotal := 0
	wdPoStSrvUsed := 0

	for _, r := range _remotes {
		if r.cfg.Commit2Srv {
			commit2SrvTotal++
			if r.limitParallel(WorkerCommit2, true) {
				commit2SrvUsed++
			}
		}

		if r.cfg.WnPoStSrv {
			wnPoStSrvTotal++
			if r.limitParallel(WorkerWinningPoSt, true) {
				wnPoStSrvUsed++
			}
		}

		if r.cfg.WdPoStSrv {
			wdPoStSrvTotal++
			if r.limitParallel(WorkerWindowPoSt, true) {
				wdPoStSrvUsed++
			}
		}

		if r.cfg.ParallelAddPiece+r.cfg.ParallelPrecommit1+r.cfg.ParallelPrecommit2+r.cfg.ParallelCommit1+r.cfg.ParallelCommit2 > 0 {
			sealWorkerTotal++
			if len(r.busyOnTasks) > 0 {
				sealWorkerLocked++
			}
			for _, val := range r.busyOnTasks {
				if val.Type%10 == 0 {
					sealWorkerUsing++
					break
				}
			}
		}
	}

	return WorkerStats{
		LocalFree:     0,
		LocalReserved: 1,
		LocalTotal:    1,

		SealWorkerTotal:  sealWorkerTotal,
		SealWorkerUsing:  sealWorkerUsing,
		SealWorkerLocked: sealWorkerLocked,
		Commit2SrvTotal:  commit2SrvTotal,
		Commit2SrvUsed:   commit2SrvUsed,
		WnPoStSrvTotal:   wnPoStSrvTotal,
		WnPoStSrvUsed:    wnPoStSrvUsed,
		WdPoStSrvTotal:   wdPoStSrvTotal,
		WdPoStSrvUsed:    wdPoStSrvUsed,

		AddPieceWait:   int(atomic.LoadInt32(&_addPieceWait)),
		PreCommit1Wait: int(atomic.LoadInt32(&_precommit1Wait)),
		PreCommit2Wait: int(atomic.LoadInt32(&_precommit2Wait)),
		Commit1Wait:    int(atomic.LoadInt32(&_commit1Wait)),
		Commit2Wait:    int(atomic.LoadInt32(&_commit2Wait)),
		FinalizeWait:   int(atomic.LoadInt32(&_finalizeWait)),
		UnsealWait:     int(atomic.LoadInt32(&_unsealWait)),
	}
}

type WorkerRemoteStatsArr []WorkerRemoteStats

func (arr WorkerRemoteStatsArr) Len() int {
	return len(arr)
}
func (arr WorkerRemoteStatsArr) Swap(i, j int) {
	arr[i], arr[j] = arr[j], arr[i]
}
func (arr WorkerRemoteStatsArr) Less(i, j int) bool {
	return arr[i].ID < arr[j].ID
}
func (sb *Sealer) WorkerRemoteStats() ([]WorkerRemoteStats, error) {
	_remoteLk.RLock()
	defer _remoteLk.RUnlock()

	result := WorkerRemoteStatsArr{}
	for _, r := range _remotes {
		sectors, err := sb.TaskWorking(r.cfg.ID)
		if err != nil {
			return nil, errors.As(err)
		}
		busyOn := []string{}
		for _, b := range r.busyOnTasks {
			busyOn = append(busyOn, b.GetSectorID())
		}
		result = append(result, WorkerRemoteStats{
			ID:       r.cfg.ID,
			IP:       r.cfg.IP,
			Disable:  r.disable,
			Srv:      r.cfg.Commit2Srv || r.cfg.WnPoStSrv || r.cfg.WdPoStSrv,
			BusyOn:   fmt.Sprintf("%+v", busyOn),
			SectorOn: sectors,
		})
	}
	sort.Sort(result)
	return result, nil
}

func (sb *Sealer) SetAddPieceListener(l func(WorkerTask)) error {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	_addPieceListener = l
	return nil
}

func (sb *Sealer) pubAddPieceEvent(t WorkerTask) {
	if _addPieceListener != nil {
		go _addPieceListener(t)
	}
}

func (sb *Sealer) DelWorker(ctx context.Context, workerId string) {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	delete(_remotes, workerId)
}

func (sb *Sealer) DisableWorker(ctx context.Context, wid string, disable bool) error {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	if err := database.DisableWorker(wid, disable); err != nil {
		return errors.As(err, wid, disable)
	}
	r, ok := _remotes[wid]
	if ok {
		r.disable = disable
	}
	return nil
}

func (sb *Sealer) AddWorker(oriCtx context.Context, cfg WorkerCfg) (<-chan WorkerTask, error) {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	if len(cfg.ID) == 0 {
		return nil, errors.New("Worker ID not found").As(cfg)
	}
	if old, ok := _remotes[cfg.ID]; ok {
		if old.release != nil {
			old.release()
		}
		return nil, errors.New("The worker has exist").As(old.cfg)
	}

	// update state in db
	if err := database.OnlineWorker(&database.WorkerInfo{
		ID:         cfg.ID,
		UpdateTime: time.Now(),
		Ip:         cfg.IP,
		SvcUri:     cfg.SvcUri,
		Online:     true,
	}); err != nil {
		return nil, errors.As(err)
	}

	wInfo, err := database.GetWorkerInfo(cfg.ID)
	if err != nil {
		return nil, errors.As(err)
	}
	taskCh := make(chan WorkerTask)
	ctx, cancel := context.WithCancel(oriCtx)
	r := &remote{
		cfg:            cfg,
		release:        cancel,
		precommit1Chan: make(chan workerCall, 10),
		precommit2Chan: make(chan workerCall, 10),
		commit1Chan:    make(chan workerCall, 10),
		commit2Chan:    make(chan workerCall, 10),
		finalizeChan:   make(chan workerCall, 10),

		sealTasks:   taskCh,
		busyOnTasks: map[string]WorkerTask{},
		disable:     wInfo.Disable,
	}
	if _, err := r.checkCache(true, nil); err != nil {
		return nil, errors.As(err, cfg)
	}
	_remotes[cfg.ID] = r

	go sb.remoteWorker(ctx, r, cfg)

	return taskCh, nil
}

// call UnlockService to release
func (sb *Sealer) selectGPUService(ctx context.Context, sid string, task WorkerTask) (*remote, error) {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	// select a remote worker
	var r *remote
	for _, _r := range _remotes {
		switch task.Type {
		case WorkerCommit2:
			if !_r.cfg.Commit2Srv {
				continue
			}
		case WorkerWinningPoSt:
			if !_r.cfg.WnPoStSrv {
				continue
			}
		case WorkerWindowPoSt:
			if !_r.cfg.WdPoStSrv {
				continue
			}
		}
		if _r.LimitParallel(task.Type, true) {
			// r is nil
			continue
		}
		r = _r
		r.lk.Lock()
		r.busyOnTasks[sid] = task
		r.lk.Unlock()
		break
	}
	if r == nil {
		return nil, errors.ErrNoData.As(sid, task)
	}
	return r, nil
}

func (sb *Sealer) UnlockGPUService(ctx context.Context, workerId, taskKey string) error {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	r, ok := _remotes[workerId]
	if !ok {
		log.Warnf("worker not found:%s", workerId)
		return nil
	}
	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err, workerId, taskKey)
	}

	r.lk.Lock()
	if !r.freeTask(sid) {
		r.lk.Unlock()
		// worker has free
		return nil
	}
	r.lk.Unlock()

	return nil
}

func (sb *Sealer) UpdateSectorState(sid, memo string, state int) error {
	sInfo, err := database.GetSectorInfo(sid)
	if err != nil {
		return errors.As(err, sid, memo, state)
	}

	// working check
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	for _, r := range _remotes {
		r.lk.Lock()
		task, ok := r.busyOnTasks[sid]
		if ok {
			if task.Type%10 == 0 {
				r.lk.Unlock()
				return errors.New("task in working").As(sid, memo, state, task.Type)
			} else {
				// free memory
				delete(r.busyOnTasks, sid)
			}
		}
		r.lk.Unlock()
	}

	// update state
	if err := database.UpdateSectorState(sid, sInfo.WorkerId, memo, state); err != nil {
		return errors.As(err)
	}
	return nil
}

func (sb *Sealer) GcWorker(invalidTime time.Time) ([]database.SectorInfo, error) {
	dropTasks, err := database.GetTimeoutTask(invalidTime)
	if err != nil {
		return nil, errors.As(err)
	}
	for _, dropTask := range dropTasks {
		// need free by manu, because precomit will burn gas.
		//if err := database.UpdateSectorState(dropTask.ID, dropTask.WorkerId, "GC task", database.SECTOR_STATE_FAILED); err != nil {
		//	return nil, errors.As(err)
		//}
		_remoteLk.Lock()
		r, ok := _remotes[dropTask.WorkerId]
		if !ok {
			_remoteLk.Unlock()
			continue
		}
		_remoteLk.Unlock()

		r.lk.Lock()
		r.freeTask(dropTask.ID)
		r.lk.Unlock()

	}
	return dropTasks, nil
}

// export for rpc service to notiy in pushing stage
func (sb *Sealer) UnlockWorker(ctx context.Context, workerId, taskKey, memo string, state int) error {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()
	r, ok := _remotes[workerId]
	if !ok {
		log.Warnf("worker not found:%s", workerId)
		return nil
	}
	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err, workerId, taskKey, memo)
	}

	r.lk.Lock()
	if !r.freeTask(sid) {
		r.lk.Unlock()
		// worker has free
		return nil
	}
	r.lk.Unlock()

	log.Infof("Release task by UnlockWorker:%s, %+v", taskKey, r.cfg)
	// release and waiting the next
	if err := database.UpdateSectorState(sid, workerId, memo, state); err != nil {
		return errors.As(err)
	}
	return nil
}

func (sb *Sealer) LockWorker(ctx context.Context, workerId, taskKey, memo string, status int) error {
	_remoteLk.Lock()
	defer _remoteLk.Unlock()

	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err)
	}

	// save worker id
	if err := database.UpdateSectorState(sid, workerId, memo, status); err != nil {
		return errors.As(err, taskKey, memo)
	}
	// TODO: busy to r.busyOnTask?
	return nil
}

func (sb *Sealer) errTask(task workerCall, err error) SealRes {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return SealRes{
		Type:   task.task.Type,
		TaskID: task.task.Key(),

		Err:   errStr,
		GoErr: err,
		WorkerCfg: WorkerCfg{
			ID: task.task.SectorStorage.WorkerInfo.ID,
			IP: task.task.SectorStorage.WorkerInfo.Ip,
		},
	}
}

func (sb *Sealer) toRemoteFree(task workerCall) {
	if len(task.task.WorkerID) > 0 {
		sb.returnTask(task)
		return
	}
	for _, r := range _remotes {
		r.lk.Lock()
		if r.disable || len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
			r.lk.Unlock()
			continue
		}
		r.lk.Unlock()

		switch task.task.Type {
		case WorkerPreCommit1:
			if int(r.precommit1Wait) < r.cfg.ParallelPrecommit1 {
				sb.toRemoteChan(task, r)
				return
			}
		case WorkerPreCommit2:
			if int(r.precommit2Wait) < r.cfg.ParallelPrecommit2 {
				sb.toRemoteChan(task, r)
				return
			}
		}
	}
	sb.returnTask(task)
}

func (sb *Sealer) toRemoteOwner(task workerCall) {
	_remoteLk.Lock()
	r, ok := _remotes[task.task.WorkerID]
	if !ok {
		_remoteLk.Unlock()
		log.Warnf(
			"no worker(%s,%s) toOwner, return task:%s",
			task.task.WorkerID, task.task.SectorStorage.WorkerInfo.Ip, task.task.Key(),
		)
		sb.returnTask(task)
		return
	}
	_remoteLk.Unlock()
	sb.toRemoteChan(task, r)
}

func (sb *Sealer) toRemoteChan(task workerCall, r *remote) {
	switch task.task.Type {
	case WorkerPreCommit1:
		atomic.AddInt32(&_precommit1Wait, 1)
		atomic.AddInt32(&(r.precommit1Wait), 1)
		r.precommit1Chan <- task
	case WorkerPreCommit2:
		atomic.AddInt32(&_precommit2Wait, 1)
		atomic.AddInt32(&(r.precommit2Wait), 1)
		r.precommit2Chan <- task
	case WorkerCommit1:
		atomic.AddInt32(&_commit1Wait, 1)
		r.commit1Chan <- task
	case WorkerCommit2:
		atomic.AddInt32(&_commit2Wait, 1)
		r.commit2Chan <- task
	case WorkerFinalize:
		atomic.AddInt32(&_finalizeWait, 1)
		r.finalizeChan <- task
	default:
		sb.returnTask(task)
	}
	return
}
func (sb *Sealer) returnTask(task workerCall) {
	var ret chan workerCall
	switch task.task.Type {
	case WorkerAddPiece:
		time.Sleep(30e9) // sleep for a while, so othe worker can get from the addPieceWait to produce addpiece.
		atomic.AddInt32(&_addPieceWait, 1)
		ret = _addPieceTasks
	case WorkerPreCommit1:
		atomic.AddInt32(&_precommit1Wait, 1)
		ret = _precommit1Tasks
	case WorkerPreCommit2:
		atomic.AddInt32(&_precommit2Wait, 1)
		ret = _precommit2Tasks
	case WorkerCommit1:
		atomic.AddInt32(&_commit1Wait, 1)
		ret = _commit1Tasks
	case WorkerCommit2:
		atomic.AddInt32(&_commit2Wait, 1)
		ret = _commit2Tasks
	case WorkerFinalize:
		atomic.AddInt32(&_finalizeWait, 1)
		ret = _finalizeTasks
	default:
		log.Error("unknown task type", task.task.Type)
	}

	go func() {
		// need sleep for the return task, or it will fall in a loop.
		time.Sleep(30e9)
		select {
		case ret <- task:
		case <-sb.stopping:
			return
		}
	}()
}

func (sb *Sealer) remoteWorker(ctx context.Context, r *remote, cfg WorkerCfg) {
	log.Infof("DEBUG:remoteWorker in:%+v", cfg)
	defer log.Infof("remote worker out:%+v", cfg)

	defer func() {
		_remoteLk.Lock()
		// offline worker
		if err := database.OfflineWorker(cfg.ID); err != nil {
			log.Error(errors.As(err))
		}
		delete(_remotes, cfg.ID)
		_remoteLk.Unlock()
	}()
	addPieceTasks := _addPieceTasks
	precommit1Tasks := _precommit1Tasks
	precommit2Tasks := _precommit2Tasks
	commit1Tasks := _commit1Tasks
	commit2Tasks := _commit2Tasks
	finalizeTasks := _finalizeTasks
	if cfg.ParallelAddPiece == 0 {
		addPieceTasks = nil
	}
	if cfg.ParallelPrecommit1 == 0 {
		precommit1Tasks = nil
		r.precommit1Chan = nil
	}
	if cfg.ParallelPrecommit2 == 0 {
		precommit2Tasks = nil
		r.precommit2Chan = nil
	}

	checkAddPiece := func() {
		// search checking is the remote busying
		r.lk.Lock()
		if r.fullTask() || r.disable {
			r.lk.Unlock()
			//log.Infow("DEBUG:", "fullTask", len(r.busyOnTasks), "maxTask", r.maxTaskNum)
			return
		}
		if r.limitParallel(WorkerAddPiece, false) {
			r.lk.Unlock()
			//log.Infof("limit parallel-addpiece:%s", r.cfg.ID)
			return
		}
		if fullCache, err := r.checkCache(false, nil); err != nil {
			r.lk.Unlock()
			log.Error(errors.As(err))
			return
		} else if fullCache {
			r.lk.Unlock()
			//log.Infof("checkAddPiece fullCache:%s", r.cfg.ID)
			return
		}
		r.lk.Unlock()

		//log.Infof("checkAddPiece:%d,queue:%d", _addPieceWait, len(_addPieceTasks))

		select {
		case task := <-addPieceTasks:
			atomic.AddInt32(&_addPieceWait, -1)
			sb.pubAddPieceEvent(task.task)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkPreCommit1 := func() {
		// search checking is the remote busying
		if r.LimitParallel(WorkerPreCommit1, false) {
			//log.Infof("limit parallel-precommit:%s", r.cfg.ID)
			return
		}

		select {
		case task := <-r.precommit1Chan:
			atomic.AddInt32(&_precommit1Wait, -1)
			atomic.AddInt32(&(r.precommit1Wait), -1)
			sb.doSealTask(ctx, r, task)
		case task := <-precommit1Tasks:
			atomic.AddInt32(&_precommit1Wait, -1)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkPreCommit2 := func() {
		if r.LimitParallel(WorkerPreCommit2, false) {
			return
		}
		select {
		case task := <-r.precommit2Chan:
			atomic.AddInt32(&_precommit2Wait, -1)
			atomic.AddInt32(&(r.precommit2Wait), -1)
			sb.doSealTask(ctx, r, task)
		case task := <-precommit2Tasks:
			atomic.AddInt32(&_precommit2Wait, -1)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkCommit1 := func() {
		// log.Infof("checkCommit:%s", r.cfg.ID)
		if r.LimitParallel(WorkerCommit1, false) {
			return
		}
		select {
		case task := <-r.commit1Chan:
			atomic.AddInt32(&_commit1Wait, -1)
			sb.doSealTask(ctx, r, task)
		case task := <-commit1Tasks:
			atomic.AddInt32(&_commit1Wait, -1)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkCommit2 := func() {
		// log.Infof("checkCommit:%s", r.cfg.ID)
		if r.LimitParallel(WorkerCommit2, false) {
			return
		}
		select {
		case task := <-r.commit2Chan:
			atomic.AddInt32(&_commit2Wait, -1)
			sb.doSealTask(ctx, r, task)
		case task := <-commit2Tasks:
			atomic.AddInt32(&_commit2Wait, -1)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkFinalize := func() {
		// for debug
		//log.Infof("finalize wait:%d,rQueue:%d,gQueue:%d", _finalizeWait, len(r.finalizeChan), len(finalizeTasks))
		if r.LimitParallel(WorkerFinalize, false) {
			return
		}

		select {
		case task := <-r.finalizeChan:
			atomic.AddInt32(&_finalizeWait, -1)
			sb.doSealTask(ctx, r, task)
		case task := <-finalizeTasks:
			atomic.AddInt32(&_finalizeWait, -1)
			sb.doSealTask(ctx, r, task)
		default:
			// nothing in chan
		}
	}
	checkFunc := []func(){
		checkFinalize, checkCommit2, checkCommit1, checkPreCommit2, checkPreCommit1, checkAddPiece,
	}

	timeout := 10 * time.Second
	for {
		// log.Info("Remote Worker Daemon")
		// priority: commit, precommit, addpiece
		select {
		case <-ctx.Done():
			log.Info("DEBUG: remoteWorker ctx done")
			return
		case <-sb.stopping:
			log.Info("DEBUG: remoteWorker stopping")
			return
		default:
			// sleep for controlling the loop
			time.Sleep(timeout)
			for _, check := range checkFunc {
				for i := 0; i < r.cfg.MaxTaskNum; i++ {
					check()
				}
			}
		}
	}
}

// return the if retask
func (sb *Sealer) doSealTask(ctx context.Context, r *remote, task workerCall) {
	// Get status in database
	ss, err := database.GetSectorStorage(task.task.GetSectorID())
	if err != nil {
		log.Error(errors.As(err))
		sb.returnTask(task)
		return
	}
	task.task.WorkerID = ss.SectorInfo.WorkerId
	task.task.SectorStorage = *ss

	if task.task.SectorID.Miner == 0 {
		// status done, and drop the data.
		err := errors.New("Miner is zero")
		log.Warn(err)
		task.ret <- sb.errTask(task, err)
		return
	}
	// status done, drop data
	if ss.SectorInfo.State > database.SECTOR_STATE_DONE {
		// status done, and drop the data.
		err := ErrTaskDone.As(*ss)
		log.Warn(err)
		task.ret <- sb.errTask(task, err)
		return
	}

	r.lk.Lock()
	switch task.task.Type {
	case WorkerAddPiece:
		if r.fullTask() {
			r.lk.Unlock()
			log.Warnf("return task:%s", r.cfg.ID, task.task.Key())
			sb.returnTask(task)
			return
		}
	case WorkerPreCommit1:
		// checking owner
		if task.task.SectorStorage.SectorInfo.State < database.SECTOR_STATE_MOVE &&
			task.task.WorkerID != r.cfg.ID &&
			// come from storage market, the workerID is empty.
			task.task.WorkerID != "" {
			// checking the owner is it exist, if exist, transfer to the owner.
			_remoteLk.Lock()
			owner, ok := _remotes[task.task.WorkerID]
			_remoteLk.Unlock()
			if ok && owner.precommit1Chan != nil {
				r.lk.Unlock()
				sb.toRemoteOwner(task)
				return
			}

			log.Warnf("Worker(%s) not found or functoin(%d) closed , waiting online or go to change the task:%+v", task.task.WorkerID, task.task.Type, task.task)
			r.lk.Unlock()
			sb.returnTask(task)
			return
		}
		if (r.disable || r.fullTask()) && !r.busyOn(task.task.GetSectorID()) {
			log.Infof("Worker(%s,%s) is in full tasks:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			r.lk.Unlock()
			sb.toRemoteFree(task)
			return
		}

		// because on miner start, the busyOn is not exact, so, need to check the database for cache.
		if fullCache, err := r.checkCache(false, []string{task.task.GetSectorID()}); err != nil {
			r.lk.Unlock()
			log.Error(errors.As(err))
			sb.returnTask(task)
			return
		} else if fullCache {
			// no cache to make a new task.
			log.Infof("Worker(%s,%s) in full cache:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			r.lk.Unlock()
			sb.toRemoteFree(task)
			return
		}
	default:
		// not the task owner
		if (task.task.SectorStorage.SectorInfo.State < database.SECTOR_STATE_MOVE ||
			task.task.SectorStorage.SectorInfo.State == database.SECTOR_STATE_PUSH) && task.task.WorkerID != r.cfg.ID {
			r.lk.Unlock()
			sb.toRemoteOwner(task)
			return
		}

		if task.task.Type != WorkerFinalize && r.fullTask() && !r.busyOn(task.task.GetSectorID()) {
			log.Infof("Worker(%s,%s) in full working:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			// remote worker is locking for the task, and should not accept a new task.
			r.lk.Unlock()
			sb.toRemoteFree(task)
			return
		}
	}
	// update status
	if err := database.UpdateSectorState(ss.SectorInfo.ID, r.cfg.ID, "task in", int(task.task.Type)); err != nil {
		log.Warn(err)
		task.ret <- sb.errTask(task, err)
		return
	}
	// make worker busy
	r.busyOnTasks[task.task.GetSectorID()] = task.task
	r.lk.Unlock()

	go func() {
		res, interrupt := sb.TaskSend(ctx, r, task.task)
		if interrupt {
			log.Warnf(
				"context expired while waiting for sector %s: %s, %s, %s",
				task.task.Key(), task.task.WorkerID, r.cfg.ID, ctx.Err(),
			)
			sb.returnTask(task)
			return
		}
		// Reload state because the state should change in TaskSend
		ss, err := database.GetSectorStorage(task.task.GetSectorID())
		if err != nil {
			log.Error(errors.As(err))
			sb.returnTask(task)
			return
		}
		task.task.WorkerID = r.cfg.ID
		task.task.SectorStorage = *ss

		sectorId := res.SectorID()
		r.lk.Lock()
		if res.GoErr != nil || len(res.Err) > 0 {
			// ignore error and do retry until cancel task by manully.
		} else if task.task.Type == WorkerFinalize {
			// make a link to storage
			if err := sb.MakeLink(&task.task); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
			if err := database.UpdateSectorState(
				sectorId, r.cfg.ID,
				"commit2 done", database.SECTOR_STATE_DONE); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
		} else if ss.SectorInfo.State < database.SECTOR_STATE_MOVE {
			state := int(res.Type) + 1
			if err := database.UpdateSectorState(
				sectorId, r.cfg.ID,
				"mission done", state); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
		}
		r.lk.Unlock()

		// send the result back to the caller
		log.Infof("Got task ret:%s", res.TaskID)
		select {
		case <-ctx.Done():
			log.Warnf(
				"context expired while waiting for sector %s: %s, %s, %s",
				task.task.Key(), task.task.WorkerID, r.cfg.ID, ctx.Err(),
			)
			sb.returnTask(task)
			return
		case <-sb.stopping:
			return
		case task.ret <- res:
			log.Infof("Got task ret done:%s", res.TaskID)
			return
		}
	}()
	return
}

func (sb *Sealer) TaskSend(ctx context.Context, r *remote, task WorkerTask) (res SealRes, interrupt bool) {
	taskKey := task.Key()
	resCh := make(chan SealRes)

	_remoteLk.Lock()
	if _, ok := _remoteResults[taskKey]; ok {
		_remoteLk.Unlock()
		// should not reach here
		panic(task)
	}
	_remoteResults[taskKey] = resCh
	_remoteLk.Unlock()

	defer func() {
		state := int(task.Type) + 1
		r.UpdateTask(task.GetSectorID(), state) // set state to done

		_remoteLk.Lock()
		// log.Infof("Delete remoteResults :%s", taskKey)
		delete(_remoteResults, taskKey)
		_remoteLk.Unlock()
	}()

	// send the task to daemon work.
	log.Infof("DEBUG: send task %s to %s", task.Key(), r.cfg.ID)
	select {
	case <-ctx.Done():
		return SealRes{}, true
	case r.sealTasks <- task:
		log.Infof("DEBUG: send task %s to %s done", task.Key(), r.cfg.ID)
	}

	// wait for the TaskDone called
	select {
	case <-ctx.Done():
		return SealRes{}, true
	case <-sb.stopping:
		return SealRes{}, true
	case res := <-resCh:
		return res, false
	}
}

// export for rpc service
func (sb *Sealer) TaskDone(ctx context.Context, res SealRes) error {
	_remoteLk.Lock()
	rres, ok := _remoteResults[res.TaskID]
	_remoteLk.Unlock()
	if !ok {
		return errors.ErrNoData.As(res.TaskID)
	}
	if rres == nil {
		log.Errorf("Not expect here:%+v", res)
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case rres <- res:
		return nil
	}
}

// export for rpc service to syncing which tasks are working.
func (sb *Sealer) TaskWorking(workerId string) (database.WorkingSectors, error) {
	return database.GetWorking(workerId)
}
func (sb *Sealer) TaskWorkingById(sid []string) (database.WorkingSectors, error) {
	return database.CheckWorkingById(sid)
}

// just implement the interface
func (sb *Sealer) CheckProvable(ctx context.Context, spt abi.RegisteredProof, sectors []abi.SectorID) ([]abi.SectorID, error) {
	panic("Should not call at here")
}
