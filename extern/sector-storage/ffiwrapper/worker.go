package ffiwrapper

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

var workerConnLock = sync.Mutex{}

func (sb *Sealer) AddWorkerConn(id string, num int) error {
	workerConnLock.Lock()
	defer workerConnLock.Unlock()
	r, ok := _remotes.Load(id)
	if ok {
		r.(*remote).srvConn += int64(num)
		_remotes.Store(id, r)
	}
	return database.AddWorkerConn(id, num)

}

// prepare worker connection will auto increment the connections
func (sb *Sealer) PrepareWorkerConn(skipWid []string) (*database.WorkerInfo, error) {
	workerConnLock.Lock()
	defer workerConnLock.Unlock()

	var minConnRemote *remote
	minConns := int64(math.MaxInt64)
	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		for _, skip := range skipWid {
			if skip == r.cfg.ID {
				return true
			}
		}
		if r.cfg.ParallelCommit2 > 0 || r.cfg.Commit2Srv || r.cfg.WdPoStSrv || r.cfg.WnPoStSrv {
			if minConns > r.srvConn {
				minConnRemote = r
			}
		}
		return true
	})
	if minConnRemote == nil {
		return nil, errors.ErrNoData
	}
	workerId := minConnRemote.cfg.ID
	info, err := database.GetWorkerInfo(workerId)
	if err != nil {
		return nil, errors.As(err)
	}
	minConnRemote.srvConn++
	_remotes.Store(workerId, minConnRemote)
	database.AddWorkerConn(workerId, 1)
	return info, nil
}

func (sb *Sealer) WorkerStats() WorkerStats {
	infos, err := database.AllWorkerInfo()
	if err != nil {
		log.Error(errors.As(err))
	}
	workerOnlines := 0
	workerOfflines := 0
	workerDisabled := 0
	for _, info := range infos {
		if info.Disable {
			workerDisabled++
			continue
		}
		if info.Online {
			workerOnlines++
		} else {
			workerOfflines++
		}
	}

	// make a copy for stats
	sealWorkerTotal := 0
	sealWorkerUsing := 0
	sealWorkerLocked := 0
	commit2SrvTotal := 0
	commit2SrvUsed := 0
	wnPoStSrvTotal := 0
	wnPoStSrvUsed := 0
	wdPoStSrvTotal := 0
	wdPoStSrvUsed := 0

	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		if r.cfg.Commit2Srv {
			commit2SrvTotal++
			if r.LimitParallel(WorkerCommit2, true) {
				commit2SrvUsed++
			}
		}

		if r.cfg.WnPoStSrv {
			wnPoStSrvTotal++
			if r.LimitParallel(WorkerWinningPoSt, true) {
				wnPoStSrvUsed++
			}
		}

		if r.cfg.WdPoStSrv {
			wdPoStSrvTotal++
			if r.LimitParallel(WorkerWindowPoSt, true) {
				wdPoStSrvUsed++
			}
		}

		if r.cfg.ParallelAddPiece+r.cfg.ParallelPrecommit1+r.cfg.ParallelPrecommit2+r.cfg.ParallelCommit1+r.cfg.ParallelCommit2 > 0 {
			r.lock.Lock()
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
			r.lock.Unlock()
		}
		return true // continue
	})

	return WorkerStats{
		PauseSeal:      atomic.LoadInt32(&sb.pauseSeal),
		WorkerOnlines:  workerOnlines,
		WorkerOfflines: workerOfflines,
		WorkerDisabled: workerDisabled,

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
	return arr[i].Disable == arr[j].Disable && arr[i].Online == arr[i].Online && arr[i].ID < arr[j].ID
}
func (sb *Sealer) WorkerRemoteStats() ([]WorkerRemoteStats, error) {
	result := WorkerRemoteStatsArr{}
	infos, err := database.AllWorkerInfo()
	if err != nil {
		return nil, errors.As(err)
	}
	for _, info := range infos {
		stat := WorkerRemoteStats{
			ID:      info.ID,
			IP:      info.Ip,
			Disable: info.Disable,
		}
		on, ok := _remotes.Load(info.ID)
		if !ok {
			stat.Online = false
			result = append(result, stat)
			continue
		}

		r := on.(*remote)
		sectors, err := sb.TaskWorking(r.cfg.ID)
		if err != nil {
			return nil, errors.As(err)
		}
		busyOn := []string{}
		r.lock.Lock()
		for _, b := range r.busyOnTasks {
			busyOn = append(busyOn, b.Key())
		}
		r.lock.Unlock()

		stat.Online = true
		stat.Srv = r.cfg.Commit2Srv || r.cfg.WnPoStSrv || r.cfg.WdPoStSrv
		stat.BusyOn = fmt.Sprintf("%+v", busyOn)
		stat.SectorOn = sectors

		result = append(result, stat)
	}
	sort.Sort(result)
	return result, nil
}

func (sb *Sealer) SetAddPieceListener(l func(WorkerTask)) error {
	_addPieceListenerLk.Lock()
	defer _addPieceListenerLk.Unlock()
	_addPieceListener = l
	return nil
}

func (sb *Sealer) pubAddPieceEvent(t WorkerTask) {
	_addPieceListenerLk.Lock()
	defer _addPieceListenerLk.Unlock()
	if _addPieceListener != nil {
		go _addPieceListener(t)
	}
}

func (sb *Sealer) GetAddPieceWait() int {
	return int(atomic.LoadInt32(&_addPieceWait))
}

func (sb *Sealer) DelWorker(ctx context.Context, workerId string) {
	_remotes.Delete(workerId)
}

func (sb *Sealer) DisableWorker(ctx context.Context, wid string, disable bool) error {
	if err := database.DisableWorker(wid, disable); err != nil {
		return errors.As(err, wid, disable)
	}

	r, ok := _remotes.Load(wid)
	if ok {
		// TODO: make sync?
		r.(*remote).disable = disable
	}
	return nil
}

func (sb *Sealer) PauseSeal(ctx context.Context, pause int32) error {
	atomic.StoreInt32(&sb.pauseSeal, pause)
	return nil
}

func (sb *Sealer) AddWorker(oriCtx context.Context, cfg WorkerCfg) (<-chan WorkerTask, error) {
	if len(cfg.ID) == 0 {
		return nil, errors.New("Worker ID not found").As(cfg)
	}
	if old, ok := _remotes.Load(cfg.ID); ok {
		if old.(*remote).release != nil {
			old.(*remote).release()
		}
		return nil, errors.New("The worker has exist").As(old.(*remote).cfg)
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
		ctx:            ctx,
		cfg:            cfg,
		precommit1Chan: make(chan workerCall, 10),
		precommit2Chan: make(chan workerCall, 10),
		commit1Chan:    make(chan workerCall, 10),
		commit2Chan:    make(chan workerCall, 10),
		finalizeChan:   make(chan workerCall, 10),

		sealTasks:   taskCh,
		busyOnTasks: map[string]WorkerTask{},
		disable:     wInfo.Disable,
	}
	r.release = func() {
		cancel()

		// clean the other lock which has called by this worker.
		_remotes.Range(func(key, val interface{}) bool {
			_r := val.(*remote)
			if _r == r {
				return true
			}

			_r.lock.Lock()
			defer _r.lock.Unlock()

			r.lock.Lock()
			for sid, _ := range r.busyOnTasks {
				_, ok := _r.busyOnTasks[sid]
				if !ok {
					continue
				}
				log.Infof("clean task(%s) by worker(%s) exit", sid, _r.cfg.ID)
				delete(_r.busyOnTasks, sid)
			}
			r.lock.Unlock()
			return true
		})
	}
	if _, err := r.checkCache(true, nil); err != nil {
		return nil, errors.As(err, cfg)
	}
	_remotes.Store(cfg.ID, r)
	if cfg.ParallelAddPiece == 0 && cfg.ParallelPrecommit1 > 0 {
		_remoteMarket.Store(cfg.ID, r)
	}

	go sb.remoteWorker(ctx, r, cfg)

	return taskCh, nil
}

// call UnlockService to release
func (sb *Sealer) selectGPUService(ctx context.Context, sid string, task WorkerTask) (*remote, bool) {
	_remoteGpuLk.Lock()
	defer _remoteGpuLk.Unlock()

	// select a remote worker
	var r *remote
	_remotes.Range(func(key, val interface{}) bool {
		_r := val.(*remote)
		_r.lock.Lock()
		defer _r.lock.Unlock()
		switch task.Type {
		case WorkerCommit2:
			if !_r.cfg.Commit2Srv {
				return true
			}
		case WorkerWinningPoSt:
			if !_r.cfg.WnPoStSrv {
				return true
			}
		case WorkerWindowPoSt:
			if !_r.cfg.WdPoStSrv {
				return true
			}
		}
		if _r.limitParallel(task.Type, true) {
			// r is nil
			return true
		}

		r = _r
		r.busyOnTasks[sid] = task // make busy
		// break range
		return false
	})
	if r == nil {
		return nil, false
	}
	return r, true
}

func (sb *Sealer) UnlockGPUService(ctx context.Context, workerId, taskKey string) error {
	_remoteGpuLk.Lock()
	defer _remoteGpuLk.Unlock()

	_r, ok := _remotes.Load(workerId)
	if !ok {
		log.Warnf("worker not found:%s", workerId)
		return nil
	}
	r := _r.(*remote)

	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err, workerId, taskKey)
	}

	if !r.freeTask(sid) {
		// worker has free
		return nil
	}

	return nil
}

func (sb *Sealer) UpdateSectorState(sid, memo string, state int, force, reset bool) (bool, error) {
	sInfo, err := database.GetSectorInfo(sid)
	if err != nil {
		return false, errors.As(err, sid, memo, state)
	}

	// working check
	working := false
	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		r.lock.Lock()
		task, ok := r.busyOnTasks[sid]
		r.lock.Unlock()
		if ok {
			if task.Type%10 == 0 {
				working = true
			}
			if working && !force {
				return false
			}

			// free memory
			r.lock.Lock()
			delete(r.busyOnTasks, sid)
			r.lock.Unlock()
		}
		return true
	})

	if working && !force {
		return working, errors.New("the task is in working").As(sid)
	}

	// update state
	newState := state
	if !reset {
		// state already done
		if sInfo.State >= 200 {
			return working, nil
		}

		newState = newState + sInfo.State
	}

	if err := database.UpdateSectorState(sid, sInfo.WorkerId, memo, newState); err != nil {
		return working, errors.As(err)
	}

	return working, nil
}

// len(workerId) == 0 for gc all workers
func (sb *Sealer) GcWorker(workerId string) ([]string, error) {
	var gc = func(r *remote) ([]string, error) {
		r.lock.Lock()
		defer r.lock.Unlock()

		result := []string{}
		for sid, task := range r.busyOnTasks {
			state, err := database.GetSectorState(sid)
			if err != nil {
				if errors.ErrNoData.Equal(err) {
					continue
				}
				return nil, errors.As(err)
			}
			if state < 200 {
				continue
			}
			delete(r.busyOnTasks, sid)
			result = append(result, fmt.Sprintf("%s,cause by: %d", task.Key(), state))
		}
		return result, nil
	}

	// for one worker
	if len(workerId) > 0 {
		val, ok := _remotes.Load(workerId)
		if !ok {
			return nil, errors.New("worker not online").As(workerId)
		}
		return gc(val.(*remote))
	}

	// for all workers
	result := []string{}
	var gErr error
	_remotes.Range(func(key, val interface{}) bool {
		ret, err := gc(val.(*remote))
		if err != nil {
			gErr = err
			return false
		}
		result = append(result, ret...)
		return true
	})

	return result, gErr
}

// export for rpc service to notiy in pushing stage
func (sb *Sealer) UnlockWorker(ctx context.Context, workerId, taskKey, memo string, state int) error {
	_r, ok := _remotes.Load(workerId)
	if !ok {
		log.Warnf("worker not found:%s", workerId)
		return nil
	}
	r := _r.(*remote)
	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err, workerId, taskKey, memo)
	}

	if !r.freeTask(sid) {
		// worker has free
		return nil
	}

	log.Infof("Release task by UnlockWorker:%s, %+v", taskKey, r.cfg)
	// release and waiting the next
	if err := database.UpdateSectorState(sid, workerId, memo, state); err != nil {
		return errors.As(err)
	}
	return nil
}

func (sb *Sealer) LockWorker(ctx context.Context, workerId, taskKey, memo string, status int) error {
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
	sent := false
	// for market
	_remoteMarket.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		r.lock.Lock()
		if r.disable || len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
			r.lock.Unlock()
			return true
		}
		r.lock.Unlock()

		switch task.task.Type {
		case WorkerPreCommit1:
			if int(r.precommit1Wait) < r.cfg.ParallelPrecommit1 {
				sent = true
				go sb.toRemoteChan(task, r)
				return false
			}
		}
		return true
	})

	if sent {
		return
	}

	// for garbage
	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		r.lock.Lock()
		if r.disable || len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
			r.lock.Unlock()
			return true
		}
		r.lock.Unlock()

		switch task.task.Type {
		case WorkerPreCommit1:
			if int(r.precommit1Wait) < r.cfg.ParallelPrecommit1 {
				sent = true
				go sb.toRemoteChan(task, r)
				return false
			}
		case WorkerPreCommit2:
			if int(r.precommit2Wait) < r.cfg.ParallelPrecommit2 {
				sent = true
				go sb.toRemoteChan(task, r)
				return false
			}
		}
		return true
	})
	if sent {
		return
	}
	sb.returnTask(task)
}

func (sb *Sealer) toRemoteOwner(task workerCall) {
	r, ok := _remotes.Load(task.task.WorkerID)
	if !ok {
		log.Warnf(
			"no worker(%s,%s) for toOwner, return task:%s",
			task.task.WorkerID, task.task.SectorStorage.WorkerInfo.Ip, task.task.Key(),
		)
		sb.returnTask(task)
		return
	}
	sb.toRemoteChan(task, r.(*remote))
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
		if r.release != nil {
			r.release()
		}
		_remoteMarket.Delete(cfg.ID)
		_remotes.Delete(cfg.ID)
		// offline worker
		if err := database.OfflineWorker(cfg.ID); err != nil {
			log.Error(errors.As(err))
		}
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
		if r.fullTask() || r.disable {
			//log.Infow("DEBUG:", "fullTask", len(r.busyOnTasks), "maxTask", r.maxTaskNum)
			return
		}
		if r.LimitParallel(WorkerAddPiece, false) {
			//log.Infof("limit parallel-addpiece:%s", r.cfg.ID)
			return
		}
		if fullCache, err := r.checkCache(false, nil); err != nil {
			log.Error(errors.As(err))
			return
		} else if fullCache {
			//log.Infof("checkAddPiece fullCache:%s", r.cfg.ID)
			return
		}

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
		checkCommit2, checkCommit1, checkPreCommit2, checkPreCommit1, checkAddPiece,
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
			for i := 0; i < r.cfg.MaxTaskNum; i++ {
				checkFinalize()
				if atomic.LoadInt32(&sb.pauseSeal) != 0 {
					// pause the seal
					continue
				}

				for _, check := range checkFunc {
					check()
				}
			}
		}
	}
}

// return the if retask
func (sb *Sealer) doSealTask(ctx context.Context, r *remote, task workerCall) {
	// Get status in database
	ss, err := database.GetSectorStorage(task.task.SectorName())
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

	switch task.task.Type {
	case WorkerAddPiece:
		if r.fullTask() {
			time.Sleep(30e9)
			log.Warnf("return task:%s", r.cfg.ID, task.task.Key())
			sb.returnTask(task)
			return
		}
		// can be scheduled
	case WorkerPreCommit1:
		// schedule to market remotes
		if len(task.task.WorkerID) == 0 && r.cfg.ParallelAddPiece > 0 && hasMarketRemotes() {
			go sb.toRemoteFree(task)
			return
		}

		// checking owner
		if task.task.SectorStorage.SectorInfo.State < database.SECTOR_STATE_MOVE &&
			task.task.WorkerID != r.cfg.ID &&
			// come from storage market, the workerID is empty.
			task.task.WorkerID != "" {
			// checking the owner is it exist, if exist, transfer to the owner.
			owner, ok := _remotes.Load(task.task.WorkerID)
			if ok && owner.(*remote).precommit1Chan != nil {
				go sb.toRemoteOwner(task)
				return
			}

			log.Warnf("Worker(%s) not found or functoin(%d) closed , waiting online or go to change the task:%+v", task.task.WorkerID, task.task.Type, task.task)
			go sb.returnTask(task)
			return
		}
		if (r.disable || r.fullTask()) && !r.busyOn(task.task.SectorName()) {
			log.Infof("Worker(%s,%s) is in full tasks:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			go sb.toRemoteFree(task)
			return
		}

		// because on miner start, the busyOn is not exact, so, need to check the database for cache.
		if fullCache, err := r.checkCache(false, []string{task.task.SectorName()}); err != nil {
			log.Error(errors.As(err))
			go sb.returnTask(task)
			return
		} else if fullCache {
			// no cache to make a new task.
			log.Infof("Worker(%s,%s) in full cache:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			go sb.toRemoteFree(task)
			return
		}
		// can be scheduled
	default:
		// not the task owner
		if (task.task.SectorStorage.SectorInfo.State < database.SECTOR_STATE_MOVE ||
			task.task.SectorStorage.SectorInfo.State == database.SECTOR_STATE_PUSH) && task.task.WorkerID != r.cfg.ID {
			go sb.toRemoteOwner(task)
			return
		}

		if task.task.Type != WorkerFinalize && r.fullTask() && !r.busyOn(task.task.SectorName()) {
			log.Infof("Worker(%s,%s) in full working:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			// remote worker is locking for the task, and should not accept a new task.
			go sb.toRemoteFree(task)
			return
		}
		// can be scheduled
	}
	// update status
	if err := database.UpdateSectorState(ss.SectorInfo.ID, r.cfg.ID, "task in", int(task.task.Type)); err != nil {
		log.Warn(err)
		task.ret <- sb.errTask(task, err)
		return
	}
	// make worker busy
	r.lock.Lock()
	r.busyOnTasks[task.task.SectorName()] = task.task
	r.lock.Unlock()

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
		ss, err := database.GetSectorStorage(task.task.SectorName())
		if err != nil {
			log.Error(errors.As(err))
			sb.returnTask(task)
			return
		}
		task.task.WorkerID = r.cfg.ID
		task.task.SectorStorage = *ss

		sectorId := res.SectorID()
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
			r.freeTask(sectorId)
		} else if ss.SectorInfo.State < database.SECTOR_STATE_MOVE {
			state := int(res.Type) + 1
			if err := database.UpdateSectorState(
				sectorId, r.cfg.ID,
				"mission done", state); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
		}

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

	_remoteResultLk.Lock()
	if _, ok := _remoteResult[taskKey]; ok {
		_remoteResultLk.Unlock()
		// should not reach here, retry later.
		log.Error(errors.New("Duplicate request").As(taskKey, r.cfg.ID, task))
		return SealRes{}, true
	}
	_remoteResult[taskKey] = resCh
	_remoteResultLk.Unlock()

	defer func() {
		state := int(task.Type) + 1
		r.UpdateTask(task.SectorName(), state) // set state to done

		log.Infof("Delete task result waiting :%s", taskKey)
		_remoteResultLk.Lock()
		delete(_remoteResult, taskKey)
		_remoteResultLk.Unlock()
	}()

	// send the task to daemon work.
	log.Infof("DEBUG: send task %s to %s (locked:%s)", task.Key(), r.cfg.ID, task.WorkerID)
	select {
	case <-ctx.Done():
		log.Infof("user canceled:%s", taskKey)
		return SealRes{}, true
	case <-r.ctx.Done():
		log.Infof("worker canceled:%s", taskKey)
		return SealRes{}, true
	case r.sealTasks <- task:
		log.Infof("DEBUG: send task %s to %s done", task.Key(), r.cfg.ID)
	}

	// wait for the TaskDone called
	select {
	case <-ctx.Done():
		log.Infof("user canceled:%s", taskKey)
		return SealRes{}, true
	case <-r.ctx.Done():
		log.Infof("worker canceled:%s", taskKey)
		return SealRes{}, true
	case <-sb.stopping:
		log.Infof("sb stoped:%s", taskKey)
		return SealRes{}, true
	case res := <-resCh:
		// send the result back to the caller
		log.Infof("Got task ret:%s", res.TaskID)
		return res, false
	}
}

// export for rpc service
func (sb *Sealer) TaskDone(ctx context.Context, res SealRes) error {
	_remoteResultLk.Lock()
	rres, ok := _remoteResult[res.TaskID]
	_remoteResultLk.Unlock()
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
func (sb *Sealer) CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []abi.SectorID) ([]abi.SectorID, error) {
	panic("Should not call at here")
}
