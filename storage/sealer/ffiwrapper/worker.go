package ffiwrapper

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/database"
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

// for the old worker version
func (sb *Sealer) PrepareWorkerConn() (*database.WorkerInfo, error) {
	workerConnLock.Lock()
	defer workerConnLock.Unlock()

	var available []*remote
	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		if r.cfg.ParallelCommit > 0 || r.cfg.Commit2Srv || r.cfg.WdPoStSrv || r.cfg.WnPoStSrv {
			available = append(available, r)
		}
		return true
	})

	if len(available) == 0 {
		return nil, errors.ErrNoData
	}

	// random the source for the old version
	minConnRemote := available[rand.Intn(len(available))]
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

// prepare worker connection will auto increment the connections
func (sb *Sealer) PrepareWorkerConnV1(skipWid []string) (*database.WorkerInfo, error) {
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
		if r.cfg.ParallelCommit > 0 || r.cfg.Commit2Srv || r.cfg.WdPoStSrv || r.cfg.WnPoStSrv {
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

		r, ok := _remotes.Load(info.ID)
		if ok { //连上过miner
			if r.(*remote).isOfflineState() { //当前断线
				workerOfflines++
			} else { //当前在线
				workerOnlines++
			}
		} else { //(miner重启后)从未连接miner
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
			if r.LimitParallel(WorkerCommit, true) {
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

		if r.cfg.ParallelPledge+r.cfg.ParallelPrecommit1+r.cfg.ParallelPrecommit2+r.cfg.ParallelCommit > 0 {
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

		PledgeWait:     int(atomic.LoadInt32(&_pledgeWait)),
		PreCommit1Wait: int(atomic.LoadInt32(&_precommit1Wait)),
		PreCommit2Wait: int(atomic.LoadInt32(&_precommit2Wait)),
		CommitWait:     int(atomic.LoadInt32(&_commitWait)),
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

		// for disable
		if info.Disable {
			result = append(result, stat)
			continue
		}

		if _r, ok := _remotes.Load(info.ID); ok {
			if r := _r.(*remote); !r.isOfflineState() { // for online
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
			} else { // for offline
				stat.Online = false
				result = append(result, stat)
			}
		} else { // for offline
			stat.Online = false
			result = append(result, stat)
		}
	}
	sort.Sort(result)
	return result, nil
}

func (sb *Sealer) SetPledgeListener(l func(WorkerTask)) error {
	_pledgeListenerLk.Lock()
	defer _pledgeListenerLk.Unlock()
	_pledgeListener = l
	return nil
}

func (sb *Sealer) pubPledgeEvent(t WorkerTask) {
	_pledgeListenerLk.Lock()
	defer _pledgeListenerLk.Unlock()
	if _pledgeListener != nil {
		go _pledgeListener(t)
	}
}

func (sb *Sealer) GetPledgeWait() int {
	return int(atomic.LoadInt32(&_pledgeWait))
}

func (sb *Sealer) DelWorker(ctx context.Context, workerId string) {
	if r, ok := _remotes.Load(workerId); ok {
		if rmt := r.(*remote); rmt.release != nil {
			rmt.release()
		}
	}
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

func (sb *Sealer) WorkerProducerIdle() int {
	left := 0
	_remotes.Range(func(key, val interface{}) bool {
		r := val.(*remote)

		if r.disable {
			return true
		}
		if r.offline == 1 {
			return true
		}
		if r.cfg.ParallelPledge <= 0 || r.cfg.ParallelPrecommit1 <= 0 {
			return true
		}

		idle := r.Idle()
		if idle > 0 {
			left += idle
		}
		return true
	})
	return left
}

func (sb *Sealer) AddWorker(oriCtx context.Context, cfg WorkerCfg) (<-chan WorkerTask, error) {
	if len(cfg.ID) == 0 {
		return nil, errors.New("Worker ID not found").As(cfg)
	}

	var (
		err  error
		rmt  *remote
		kind = WorkerQueueKind_MinerReStart
	)
	defer func() {
		if err != nil {
			log.Infof("AddWorker(%v): worker(%v) error(%v)", cfg.Retry, cfg.ID, err)
			return
		}

		if rmt != nil {
			rmt.dictBusyRW.Lock()
			rmt.dictBusy = make(map[string]string)
			for _, typ := range cfg.Busy {
				rmt.dictBusy[typ] = typ
			}
			rmt.dictBusyRW.Unlock()

			//1.加载busy状态
			log.Infow("AddWorker of before load-busy", "wid", cfg.ID, "retry", cfg.Retry, "worker-busy", cfg.Busy)
			if err = sb.loadBusyStatus(kind, rmt, cfg); err != nil {
				log.Infof("AddWorker(%v): worker(%v) load busy status error(%v)", cfg.Retry, cfg.ID, err)
				return
			}
			//2.设置worker为在线状态(需要放在最后一步)
			log.Infow("AddWorker of after load-busy", "wid", cfg.ID, "retry", cfg.Retry, "worker-busy", cfg.Busy)
			if err = sb.onlineWorker(oriCtx, rmt, cfg); err != nil {
				log.Infof("AddWorker(%v): worker(%v) online error(%v)", cfg.Retry, cfg.ID, err)
				return
			}
			log.Infow("AddWorker online finish", "wid", cfg.ID, "retry", cfg.Retry)
		}
	}()

	log.Infof("AddWorker(%v): worker(%v) starting...", cfg.Retry, cfg.ID)
	if old, ok := _remotes.Load(cfg.ID); ok { //1.worker在miner里面存在(比如worker重启或重连)
		rmt = old.(*remote)
		if kind = WorkerQueueKind_WorkerReStart; cfg.Retry > 0 {
			kind = WorkerQueueKind_WorkerReConnect
		}
	} else { //2.worker在miner里面不存在(miner重启或worker首次连接)
		if rmt, err = sb.initWorker(oriCtx, cfg); err != nil {
			return nil, err
		}
	}

	return rmt.sealTasks, nil
}

// worker(p1,c2)重启(或首次启动)的时候需要做一次初始化(重连的时候不需要)
func (sb *Sealer) initWorker(oriCtx context.Context, cfg WorkerCfg) (rmt *remote, err error) {
	log.Infof("init worker(%v) starting...", cfg.ID)

	//1.worker已经初始化过 则直接返回 （初始化操作只执行一次）
	if old, ok := _remotes.Load(cfg.ID); ok {
		log.Infof("init worker(%v) skiped", cfg.ID)
		return old.(*remote), nil
	}

	//2.worker init...
	defer func() {
		if err != nil {
			log.Infof("init worker(%v) error: %v", cfg.ID, err)
		} else {
			log.Infof("init worker(%v) finish", cfg.ID)
		}
	}()

	_tmpPledgeTasksRW.Lock()
	defer _tmpPledgeTasksRW.Unlock()

	ctx, cancel := context.WithCancel(context.Background()) //注意：此处不能用oriCtx作为父parent 因为worker实现了断线重连
	rmt = &remote{
		ctx:                 oriCtx, //这个上下文必须要是jsonrpc的上下文 用于捕获连接是否断开
		cfg:                 cfg,
		pledgeChan:          make(chan workerCall, len(_tmpPledgeTasks[cfg.ID])+10),
		precommit1Chan:      make(chan workerCall, 10),
		precommit2Chan:      make(chan workerCall, 10),
		commitChan:          make(chan workerCall, 10),
		finalizeChan:        make(chan workerCall, 10),
		unsealChan:          make(chan workerCall, 10),
		replicaUpdateChan:   make(chan workerCall, 10),
		pReplicaUpdate1Chan: make(chan workerCall, 10),
		pReplicaUpdate2Chan: make(chan workerCall, 10),
		fReplicaUpdateChan:  make(chan workerCall, 10),

		sealTasks:   make(chan WorkerTask),
		busyOnTasks: map[string]WorkerTask{},

		release: func() {
			log.Infof("worker(%v) release", rmt.cfg.ID)

			cancel()
			rmt.setOfflineState()
			rmt.lock.Lock()
			rmt.busyOnTasks = map[string]WorkerTask{}
			rmt.lock.Unlock()

			_remotes.Delete(cfg.ID)
		},
	}

	//加载worker连接miner之前的pledge任务
	for _, task := range _tmpPledgeTasks[cfg.ID] {
		rmt.pledgeChan <- task
	}
	_tmpPledgeTasks[cfg.ID] = make([]workerCall, 0, 0)

	_remotes.Store(cfg.ID, rmt)
	go sb.loopWorker(ctx, rmt, cfg)
	go sb.offlineWorkerLoop(ctx, rmt)

	log.Infof("worker(%v) init (busy status: %v)", cfg.ID, rmt.busyOnTasks)
	return rmt, nil
}

func (sb *Sealer) onlineWorker(oriCtx context.Context, rmt *remote, cfg WorkerCfg) error {
	if rmt == nil {
		return fmt.Errorf("remote is nil on onlineWorker")
	}

	rmt.offlineRW.Lock()
	defer rmt.offlineRW.Unlock()

	var (
		err   error
		wInfo *database.WorkerInfo
	)
	if err = database.OnlineWorker(&database.WorkerInfo{
		ID:         cfg.ID,
		UpdateTime: time.Now(),
		Ip:         cfg.IP,
		SvcUri:     cfg.SvcUri,
		Online:     true,
	}); err != nil {
		return errors.As(err)
	}
	if wInfo, err = database.GetWorkerInfo(cfg.ID); err != nil {
		return errors.As(err)
	}
	{
		rmt.ctx = oriCtx
		rmt.cfg = cfg
		rmt.disable = wInfo.Disable
		rmt.clearOfflineState()
	}
	sb.offlineWorker.Delete(cfg.ID)
	return nil
}

func (sb *Sealer) offlineWorkerLoop(ctx context.Context, rmt *remote) {
	log.Infow("offline worker loop starting", "worker-id", rmt.cfg.ID)
	defer log.Infow("offline worker loop exit", "worker-id", rmt.cfg.ID)

	for {
		<-time.After(time.Second * 3) //检测worker是否掉线的间隔
		if rmt.isOfflineState() {
			continue
		}

		rmt.offlineRW.RLock()
		rmtCtx, cycle, retry := rmt.ctx, rmt.cfg.Cycle, rmt.cfg.Retry
		rmt.offlineRW.RUnlock()

		select {
		case <-ctx.Done(): //全局退出（进程退出/DeleteWorker）
			return
		case <-rmtCtx.Done(): //worker下线
			sb.offlineWorkerHandle(rmt, cycle, retry)
		default:
		}
	}
}

func (sb *Sealer) offlineWorkerHandle(rmt *remote, cycle string, retry int) {
	if rmt == nil {
		return
	}

	rmt.offlineRW.RLock()
	defer rmt.offlineRW.RUnlock()

	if cycle != rmt.cfg.Cycle || retry != rmt.cfg.Retry {
		log.Infow("worker offline ignore", "worker-id", rmt.cfg.ID, "old-retry", retry, "curr-retry", retry)
		return
	}

	log.Infow("worker offline...", "worker-id", rmt.cfg.ID, "retry", retry)
	rmt.setOfflineState()
	sb.offlineWorker.Store(rmt.cfg.ID, rmt)
	if err := database.OfflineWorker(rmt.cfg.ID); err != nil {
		log.Errorw("worker offline error", "worker-id", rmt.cfg.ID, "retry", retry, "err", err)
	}
}

func (sb *Sealer) loadBusyStatus(kind WorkerQueueKind, rmt *remote, cfg WorkerCfg) error {
	if rmt == nil {
		return nil
	}

	//1.c2 worker不绑定sector 所以c2 worker重连的时候需要将运行中的sector信息传入 作为恢复busy的依据
	if rmt.cfg.Commit2Srv || rmt.cfg.WdPoStSrv || rmt.cfg.WnPoStSrv {
		rmt.lock.Lock()
		rmt.busyOnTasks = map[string]WorkerTask{}
		for _, sid := range cfg.C2Sids {
			task := WorkerTask{
				Type:     WorkerCommit,
				SectorID: sid,
			}
			rmt.busyOnTasks[task.SectorName()] = task
		}
		rmt.lock.Unlock()
	} else {
		//2.p1 worker从sqlite恢复busy状态(worker重连则不需要恢复)
		switch kind {
		case WorkerQueueKind_MinerReStart: //miner重启时: checkCache + worker上报的Busy状态
			//1.从sqlite恢复busy状态
			if _, err := rmt.checkCache(true, nil); err != nil {
				return err
			}
			//2.从worker上报的任务fix checkCache的结果
			rmt.checkBusy(cfg.Busy)
		case WorkerQueueKind_WorkerReStart: //worker重启时: 直接使用checkCache
			if _, err := rmt.checkCache(true, nil); err != nil {
				return err
			}
		case WorkerQueueKind_WorkerReConnect:
			rmt.checkBusy(cfg.Busy)
		}
	}

	return nil
}

// call UnlockService to release
func (sb *Sealer) selectGPUService(ctx context.Context, sid string, task WorkerTask) (*remote, bool) {
	_remoteGpuLk.Lock()
	defer _remoteGpuLk.Unlock()
	log.Infof("task(%v) select gpu starting", task.SectorID)

	var (
		r   *remote
		rs  []*remote
		msg = ""
	)
	//1.找出所有在线的c2 worker
	_remotes.Range(func(key, val interface{}) bool {
		_r := val.(*remote)
		//过滤当前断线的worker
		if _r.disable || _r.isOfflineState() {
			return true
		}
		//过滤类型不匹配的worker(如p1)
		if !_r.taskEnable(task) {
			return true
		}
		rs = append(rs, _r)
		return true
	})

	//2.根据已分发的任务数"降序"排序c2 worker
	sort.Slice(rs, func(i, j int) bool {
		return len(rs[i].busyOnTasks) > len(rs[j].busyOnTasks)
	})
	//3.根据已分发的任务数"降序"遍历c2 worker
	for _, _r := range rs {
		//3.1 优先获取之前被这个任务选择过（p1->c2连接超时）的c2
		if t, ok := _r.busyOnTasks[sid]; ok && t.Type == task.Type {
			log.Infof("task(%v) select gpu with old-hit c2worker", task.SectorID)
			r = _r
			break
		}
		//3.2 如果没有曾经下发过的c2 worker处于空闲状态 则"降序"遍历退出时选择到任务数最小的一个空闲c2 worker
		if !_r.limitParallel(task.Type, true) {
			r = _r
			//这里不能break 需要"降序"遍历到最后一条记录 以获取任务数最小的c2 worker
		}
	}
	//4.找到了c2 worker 则设置busy状态
	if msg = "not found"; r != nil {
		msg = r.cfg.ID
		r.lock.Lock()
		r.busyOnTasks[sid] = task // make busy
		r.lock.Unlock()

		if err := database.UploadSectorMonitorState(sid, r.cfg.ID, "c2 running", database.WorkerCommitRun, database.GetSectorSnapValue(task.Snap)); err != nil {
			log.Error("UpdateSectorState Err, sectorId:%d, error:%v:", task.SectorID, err)
		}
	}
	log.Infof("task(%v) select gpu finish: %v", task.SectorID, msg)

	return r, r != nil
}

func (sb *Sealer) UnlockGPUService(ctx context.Context, rst *Commit2Result) error {
	_remoteGpuLk.Lock()
	defer _remoteGpuLk.Unlock()

	_r, ok := _remotes.Load(rst.WorkerId)
	if !ok {
		return nil
	}
	r := _r.(*remote)

	sid, _, err := ParseTaskKey(rst.TaskKey)
	if err != nil {
		sid = rst.TaskKey // for service called.
	}
	if rst.Snap {
		log.Warn("pass snap result ")
	} else {
		c2cache.set(rst)
	}
	r.freeTask(sid)
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

	if err := database.UpdateSectorState(sid, sInfo.WorkerId, memo, newState, sInfo.Snap); err != nil {
		return working, errors.As(err)
	}

	return working, nil
}

func (sb *Sealer) GcTimeoutTask(stopTime time.Time) ([]string, error) {
	var gc = func(r *remote) ([]string, error) {
		r.lock.Lock()
		defer r.lock.Unlock()

		result := []string{}
		for sid, task := range r.busyOnTasks {
			sInfo, err := database.GetSectorInfo(sid)
			if err != nil {
				if errors.ErrNoData.Equal(err) {
					continue
				}
				return nil, errors.As(err)
			}
			if sInfo.State >= database.SECTOR_STATE_PUSH {
				continue
			}
			if sInfo.CreateTime.After(stopTime) {
				continue
			}
			delete(r.busyOnTasks, sid)
			newStat := sInfo.State + 500
			memo := fmt.Sprintf("%s,cause by: %d", task.Key(), newStat)

			result = append(result, memo)
			if err := database.UpdateSectorState(sid, sInfo.WorkerId, memo, newStat, sInfo.Snap); err != nil {
				return nil, errors.As(err)
			}
		}
		return result, nil
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
			if state < database.SECTOR_STATE_DONE {
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
	sInfo, err := database.GetSectorInfo(sid)
	if err != nil {
		return errors.As(err, sid, memo)
	}
	if !r.freeTask(sid) {
		// worker has free
		return nil
	}

	log.Infof("Release task by UnlockWorker:%s, %+v", taskKey, r.cfg)
	// release and waiting the next
	if err := database.UpdateSectorState(sid, workerId, memo, state, sInfo.Snap); err != nil {
		return errors.As(err)
	}
	return nil
}

func (sb *Sealer) LockWorker(ctx context.Context, workerId, taskKey, memo string, status int) error {
	sid, _, err := ParseTaskKey(taskKey)
	if err != nil {
		return errors.As(err)
	}
	sInfo, err := database.GetSectorInfo(sid)
	if err != nil {
		return errors.As(err, sid, memo)
	}
	// save worker id
	if err := database.UpdateSectorState(sid, workerId, memo, status, sInfo.Snap); err != nil {
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
	if r, ok := _remotes.Load(task.task.WorkerID); !ok {
		// clear this on worker online.
		sb.offlineWorker.Store(task.task.WorkerID, task.task.SectorStorage.WorkerInfo)

		//已绑定了worker的刷单任务 不返回全局队列（防止阻塞刷单循环）其他情况的任务都返回全局队列
		if task.task.Type == WorkerPledge && len(task.task.WorkerID) > 0 {
			_tmpPledgeTasksRW.Lock()
			_tmpPledgeTasks[task.task.WorkerID] = append(_tmpPledgeTasks[task.task.WorkerID], task)
			_tmpPledgeTasksRW.Unlock()
		} else {
			sb.returnTask(task)
		}
	} else {
		sb.toRemoteChan(task, r.(*remote))
	}
}

func (sb *Sealer) toRemoteChan(task workerCall, r *remote) {
	switch task.task.Type {
	case WorkerPledge:
		atomic.AddInt32(&(r.pledgeWait), 1)
		r.pledgeChan <- task
	case WorkerPreCommit1:
		atomic.AddInt32(&_precommit1Wait, 1)
		atomic.AddInt32(&(r.precommit1Wait), 1)
		r.precommit1Chan <- task
	case WorkerPreCommit2:
		atomic.AddInt32(&_precommit2Wait, 1)
		atomic.AddInt32(&(r.precommit2Wait), 1)
		r.precommit2Chan <- task
	case WorkerCommit:
		atomic.AddInt32(&_commitWait, 1)
		r.commitChan <- task
	case WorkerFinalize:
		atomic.AddInt32(&_finalizeWait, 1)
		r.finalizeChan <- task
	case WorkerUnseal:
		atomic.AddInt32(&_unsealWait, 1)
		r.unsealChan <- task
	case WorkerReplicaUpdate:
		atomic.AddInt32(&_replicaUpdateWait, 1)
		r.replicaUpdateChan <- task
	case WorkerProveReplicaUpdate1:
		atomic.AddInt32(&_proveReplicaUpdate1Wait, 1)
		r.pReplicaUpdate1Chan <- task
	case WorkerProveReplicaUpdate2:
		atomic.AddInt32(&_proveReplicaUpdate2Wait, 1)
		r.pReplicaUpdate2Chan <- task
	case WorkerFinalizeReplicaUpdate:
		atomic.AddInt32(&_finalizeReplicaUpdateWait, 1)
		r.fReplicaUpdateChan <- task
	default:
		sb.returnTask(task)
	}
	return
}

func (sb *Sealer) returnTask(task workerCall) {
	var ret chan workerCall
	switch task.task.Type {
	case WorkerPledge:
		atomic.AddInt32(&_pledgeWait, 1)
		ret = _pledgeTasks
	case WorkerPreCommit1:
		atomic.AddInt32(&_precommit1Wait, 1)
		ret = _precommit1Tasks
	case WorkerPreCommit2:
		atomic.AddInt32(&_precommit2Wait, 1)
		ret = _precommit2Tasks
	case WorkerCommit:
		atomic.AddInt32(&_commitWait, 1)
		ret = _commitTasks
	case WorkerFinalize:
		atomic.AddInt32(&_finalizeWait, 1)
		ret = _finalizeTasks
	case WorkerUnseal:
		atomic.AddInt32(&_unsealWait, 1)
		ret = _unsealTasks
	case WorkerReplicaUpdate:
		atomic.AddInt32(&_replicaUpdateWait, 1)
		ret = _replicaUpdateTasks
	case WorkerProveReplicaUpdate1:
		atomic.AddInt32(&_proveReplicaUpdate1Wait, 1)
		ret = _proveReplicaUpdate1Tasks
	case WorkerProveReplicaUpdate2:
		atomic.AddInt32(&_proveReplicaUpdate2Wait, 1)
		ret = _proveReplicaUpdate2Tasks
	case WorkerFinalizeReplicaUpdate:
		atomic.AddInt32(&_finalizeReplicaUpdateWait, 1)
		ret = _finalizeReplicaUpdateTasks
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

func (sb *Sealer) returnTaskWithoutCounter(task workerCall) {
	var ret chan workerCall
	switch task.task.Type {
	case WorkerPledge:
		ret = _pledgeTasks
	case WorkerPreCommit1:
		ret = _precommit1Tasks
	case WorkerPreCommit2:
		ret = _precommit2Tasks
	case WorkerCommit:
		ret = _commitTasks
	case WorkerFinalize:
		ret = _finalizeTasks
	case WorkerUnseal:
		ret = _unsealTasks
	case WorkerReplicaUpdate:
		ret = _replicaUpdateTasks
	case WorkerProveReplicaUpdate1:
		ret = _proveReplicaUpdate1Tasks
	case WorkerProveReplicaUpdate2:
		ret = _proveReplicaUpdate2Tasks
	case WorkerFinalizeReplicaUpdate:
		ret = _finalizeReplicaUpdateTasks
	default:
		log.Error("unknown task type", task.task.Type)
	}

	go func() {
		select {
		case ret <- task:
		case <-sb.stopping:
			return
		}
	}()
}

func (sb *Sealer) loopWorker(ctx context.Context, r *remote, cfg WorkerCfg) {
	log.Infof("DEBUG:remote worker in:%+v", cfg.ID)
	defer log.Infof("DEBUG:remote worker out:%+v", cfg.ID)

	pledgeTasks := _pledgeTasks
	precommit1Tasks := _precommit1Tasks
	precommit2Tasks := _precommit2Tasks
	commitTasks := _commitTasks
	finalizeTasks := _finalizeTasks
	unsealTasks := _unsealTasks

	replicaUpdateTasks := _replicaUpdateTasks
	pReplicaUpdate1Tasks := _proveReplicaUpdate1Tasks
	pReplicaUpdate2Tasks := _proveReplicaUpdate2Tasks
	fReplicaUpdateTasks := _finalizeReplicaUpdateTasks

	if cfg.ParallelPledge == 0 {
		pledgeTasks = nil
		r.pledgeChan = nil
	}
	if cfg.ParallelPrecommit1 == 0 {
		precommit1Tasks = nil
		r.precommit1Chan = nil

		// unseal is shared with the parallel-precommit1
		unsealTasks = nil
		r.unsealChan = nil
	}
	if cfg.ParallelPrecommit2 == 0 {
		precommit2Tasks = nil
		r.precommit2Chan = nil
	}

	checkPledge := func() {

		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.pledgeChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&(r.pledgeWait), -1)
				sb.pubPledgeEvent(task.task)
			}
		case task := <-pledgeTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_pledgeWait, -1)
				sb.pubPledgeEvent(task.task)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			//log.Infow("pledge task not-task", "worker-id", r.cfg.ID)
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("pledge task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		r.lock.Lock()
		_, busy := r.busyOnTasks[wc.task.SectorName()]
		r.lock.Unlock()
		log.Infow("pledge task judge...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "busy", busy, "ok", ok, "limit", !r.LimitParallel(WorkerPledge, false))
		// search checking is the remote busying
		if r.disable && (!busy || !ok) {
			log.Infow("pledge task fake", "worker-id", r.cfg.ID, "max-task", r.cfg.MaxTaskNum, "disable", r.disable)
			return
		}
		if busy || ok || !r.LimitParallel(WorkerPledge, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("pledge task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("pledge task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			wc.task.WorkerID = ""
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkPreCommit1 := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.precommit1Chan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_precommit1Wait, -1)
				atomic.AddInt32(&(r.precommit1Wait), -1)
			}
		case task := <-precommit1Tasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_precommit1Wait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			//log.Infow("p1 task not-task", "worker-id", r.cfg.ID)
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("p1 task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerPreCommit1, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("p1 task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("p1 task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkPreCommit2 := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.precommit2Chan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_precommit2Wait, -1)
				atomic.AddInt32(&(r.precommit2Wait), -1)
			}
		case task := <-precommit2Tasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_precommit2Wait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			//log.Infow("p2 task not-task", "worker-id", r.cfg.ID)
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("p2 task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerPreCommit2, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("p2 task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("p2 task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkCommit := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.commitChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_commitWait, -1)
			}
		case task := <-commitTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_commitWait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerCommit, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
		} else {
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkFinalize := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.finalizeChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_finalizeWait, -1)
			}
		case task := <-finalizeTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_finalizeWait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerFinalize, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("finalize task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			sb.returnTaskWithoutCounter(*wc)
			log.Infow("finalize task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			time.Sleep(time.Second * 3)
		}
	}
	checkUnseal := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.unsealChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_unsealWait, -1)
			}
		case task := <-unsealTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_unsealWait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerUnseal, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
		} else {
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkReplicaUpdate := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.replicaUpdateChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_replicaUpdateWait, -1)
			}
		case task := <-replicaUpdateTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_replicaUpdateWait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("RU task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerReplicaUpdate, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("RU task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("RU task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkPReplicaUpdate1 := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.pReplicaUpdate1Chan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_proveReplicaUpdate1Wait, -1)
			}
		case task := <-pReplicaUpdate1Tasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_proveReplicaUpdate1Wait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("PR1 task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerProveReplicaUpdate1, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("PR1 task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("PR1 task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkPReplicaUpdate2 := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.pReplicaUpdate2Chan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_proveReplicaUpdate2Wait, -1)
			}
		case task := <-pReplicaUpdate2Tasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_proveReplicaUpdate2Wait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("PR2 task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerProveReplicaUpdate2, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("PR2 task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("PR2 task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkFReplicaUpdate := func() {
		var (
			wc *workerCall
			fn func()
		)
		select {
		case task := <-r.fReplicaUpdateChan:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_finalizeReplicaUpdateWait, -1)
			}
		case task := <-fReplicaUpdateTasks:
			wc = &task
			fn = func() {
				atomic.AddInt32(&_finalizeReplicaUpdateWait, -1)
			}
		default:
			// nothing in chan
		}
		if wc == nil || fn == nil {
			return
		}
		//允许重复下发worker正在执行的任务(worker自己会过滤) for 断线重连后TaskDone正常工作
		log.Infow("FRU task start...", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		r.dictBusyRW.RLock()
		_, ok := r.dictBusy[wc.task.SectorName()]
		r.dictBusyRW.RUnlock()
		if ok || !r.LimitParallel(WorkerFinalizeReplicaUpdate, false) {
			fn()
			sb.doSealTask(ctx, r, *wc)
			log.Infow("FRU task do-seal", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
		} else {
			log.Infow("FRU task ignore", "worker-id", r.cfg.ID, "task-key", (*wc).task.Key(), "snap", (*wc).task.Snap)
			sb.returnTaskWithoutCounter(*wc)
			time.Sleep(time.Second * 3)
		}
	}
	checkFunc := []func(){
		checkUnseal, checkCommit, checkPreCommit2, checkPreCommit1, checkPledge,
		checkPReplicaUpdate2, checkPReplicaUpdate1, checkReplicaUpdate,
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("DEBUG: remoteWorker ctx done")
			return
		case <-sb.stopping:
			log.Info("DEBUG: remoteWorker stopping")
			return
		default:
			// sleep for controlling the loop
			time.Sleep(5 * time.Second)
			if  r.isOfflineState() || atomic.LoadInt32(&sb.pauseSeal) != 0 {
				continue
			}

			checkFinalize()
			checkFReplicaUpdate()
			for _, check := range checkFunc {
				if !r.isOfflineState() {
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
	case WorkerPledge:
		// not the task owner
		if len(task.task.WorkerID) > 0 && task.task.WorkerID != r.cfg.ID {
			log.Warnf("not the task owner return task: %v, %v", r.cfg.ID, task.task.Key())
			worker, _ := database.GetWorkerInfo(ss.SectorInfo.WorkerId)
			if worker != nil && worker.Disable {
				sInfo, _ := database.GetSectorInfo(ss.SectorInfo.ID)
				if sInfo != nil && sInfo.State == database.SECTOR_STATE_DONE {
					if err := database.UpdateSectorState(
						ss.SectorInfo.ID, "",
						fmt.Sprintf("done:%d", task.task.Type), database.SECTOR_STATE_DONE, database.GetSectorSnapValue(task.task.Snap)); err != nil {
						log.Warnf("try reset workerid task (snap) err %v: %v, %v", r.cfg.ID, task.task.Key(),err.Error())
						return
					}else {
						log.Warnf("try reset workerid task (snap) %v: %v", r.cfg.ID, task.task.Key())
					}
				}
			}
			go sb.toRemoteOwner(task)
			return
		}

		if r.fakeFullTask() && !r.busyOn(task.task.SectorName()) {
			time.Sleep(30e9)
			log.Warnf("return task: %v, %v", r.cfg.ID, task.task.Key())
			sb.returnTask(task)
			return
		}
	default:
		// not the task owner
		if len(task.task.WorkerID) > 0 && task.task.WorkerID != r.cfg.ID {
			log.Warnf("not the task owner return task: %v, %v", r.cfg.ID, task.task.Key())
			go sb.toRemoteOwner(task)
			return
		}

		if task.task.Type != WorkerFinalize && r.fakeFullTask() && !r.busyOn(task.task.SectorName()) {
			log.Infof("Worker(%s,%s) is full working:%d, return:%s", r.cfg.ID, r.cfg.IP, len(r.busyOnTasks), task.task.Key())
			// remote worker is locking for the task, and should not accept a new task.
			go sb.toRemoteFree(task)
			return
		}
		// can be scheduled

		// this is fix the bug of remove error when finalizing in v1.4.0-patch2
		if task.task.Type == WorkerFinalize && (r.cfg.Commit2Srv || r.cfg.WdPoStSrv || r.cfg.WnPoStSrv) {
			if err := database.UpdateSectorState(
				ss.SectorInfo.ID, r.cfg.ID,
				fmt.Sprintf("done:%d", task.task.Type), database.SECTOR_STATE_DONE, database.GetSectorSnapValue(task.task.Snap)); err != nil {
				task.ret <- sb.errTask(task, errors.As(err))
				return
			}
			r.freeTask(ss.SectorInfo.ID)
			task.ret <- sb.errTask(task, nil)
			return
		}
		// end fix bug
	}
	// update status
	if err := database.UpdateSectorState(ss.SectorInfo.ID, r.cfg.ID, "task in", int(task.task.Type), database.GetSectorSnapValue(task.task.Snap)); err != nil {
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
		} else if task.task.Type == WorkerFinalize || task.task.Type == WorkerUnseal || task.task.Type == WorkerFinalizeReplicaUpdate {
			// make a link to storage
			if err := sb.MakeLink(&task.task); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
			if err := database.UpdateSectorState(
				sectorId, r.cfg.ID,
				fmt.Sprintf("done:%d", task.task.Type), database.SECTOR_STATE_DONE, database.GetSectorSnapValue(task.task.Snap)); err != nil {
				res = sb.errTask(task, errors.As(err))
			}
			if err := database.UploadSectorProvingState(ss.SectorInfo.ID, database.GetSectorSnapValue(task.task.Snap)); err != nil {
				log.Errorw("upload sector proving state error", "sid", ss.SectorInfo.ID, "err", err)
			}
			r.freeTask(sectorId)
		} else if ss.SectorInfo.State < database.SECTOR_STATE_MOVE {
			state := int(res.Type) + 1
			if err := database.UpdateSectorState(
				sectorId, r.cfg.ID,
				"transfer mission", state, database.GetSectorSnapValue(task.task.Snap)); err != nil {
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
			return
		}
	}()
	return
}

func (sb *Sealer) TaskSend(ctx context.Context, r *remote, task WorkerTask) (res SealRes, interrupt bool) {
	log.Infow("task sending", "worker-id", r.cfg.ID, "task-key", task.Key(), "snap", task.Snap)

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

	r.offlineRW.RLock()
	cycle, retry := r.cfg.Cycle, r.cfg.Retry
	r.offlineRW.RUnlock()
	beginTime := time.Now()
	defer func() {
		_remoteResultLk.Lock()
		delete(_remoteResult, taskKey)
		_remoteResultLk.Unlock()

		r.offlineRW.RLock()
		cycleCurr, retryCurr := r.cfg.Cycle, r.cfg.Retry
		r.offlineRW.RUnlock()
		if cycleCurr == cycle && retryCurr == retry { //当前没有重新上线
			state := int(task.Type) + 1
			r.UpdateTask(task.SectorName(), state) // set state to done
			log.Infof("Delete task waiting :%s", taskKey)
		}

		// seal stat
		endTime := time.Now()
		sealStat := "success"
		if len(res.Err) > 0 {
			sealStat = res.Err
		} else if res.GoErr != nil {
			sealStat = res.GoErr.Error()
		}
		stSeal := &database.StatisSeal{
			TaskID:    task.Key(),
			Sid:       task.SectorName(),
			Stage:     task.Type.Stage(),
			WorkerID:  res.WorkerCfg.ID,
			BeginTime: beginTime,
			EndTime:   endTime,
			Used:      int64(endTime.Sub(beginTime).Seconds()),
			Error:     sealStat,
		}
		if err := database.PutStatisSeal(stSeal); err != nil {
			log.Warn(errors.As(err))
		}

	}()

	// send the task to daemon work.
	log.Infof("DEBUG: send task %s to %s (locked:%s) (snap:%v)", task.Key(), r.cfg.ID, task.WorkerID, task.Snap)
	select {
	case <-ctx.Done():
		log.Infof("user canceled:%s (snap:%v)", taskKey, task.Snap)
		return SealRes{}, true
	case <-r.ctx.Done():
		log.Infof("worker canceled:%s (snap:%v)", taskKey, task.Snap)
		return SealRes{}, true
	case r.sealTasks <- task:
	}

	// wait for the TaskDone called
	select {
	case <-ctx.Done():
		log.Infof("user canceled:%s (snap:%v)", taskKey, task.Snap)
		return SealRes{}, true
	case <-r.ctx.Done():
		log.Infof("worker canceled:%s (snap:%v)", taskKey, task.Snap)
		return SealRes{}, true
	case <-sb.stopping:
		log.Infof("sb stoped:%s (snap:%v)", taskKey, task.Snap)
		return SealRes{}, true
	case res := <-resCh:
		// send the result back to the caller
		log.Infof("task return result:%v (snap:%v)", taskKey, task.Snap)
		return res, false
	}
}

// export for rpc service
func (sb *Sealer) TaskDone(ctx context.Context, res SealRes) error {
	var (
		rmt     *remote
		errConn = fmt.Errorf("connection refused")
	)
	//worker重连的时候，需要先online完成 才能TaskDone 否则busy状态可能不一致
	if r, ok := _remotes.Load(res.WorkerCfg.ID); ok {
		if rmt = r.(*remote); rmt.isOfflineState() {
			return errConn
		}
	} else {
		return errConn
	}

	_remoteResultLk.Lock()
	rres, ok := _remoteResult[res.TaskID]
	_remoteResultLk.Unlock()
	if !ok { //等待fsm触发任务重做->_remoteResult[res.TaskID]有值->TaskDone成功
		return errConn
		//return errors.ErrNoData.As(res.TaskID)
	}
	if rres == nil {
		log.Errorf("Not expect here:%+v", res)
		return nil
	}

	defer func() {
		if arr := strings.Split(res.TaskID, "_"); len(arr) > 0 {
			rmt.dictBusyRW.Lock()
			delete(rmt.dictBusy, arr[0])
			rmt.dictBusyRW.Unlock()
		}
	}()
	if size := len(res.Err); size > 0 {
		log.Errorw("Task done error", "task-id", res.TaskID, "err", res.Err)
		if limit := 200; size > limit { //状态机在处理太长的错误的时候会报错 导致任务无法重做 故此处截取错误信息(200个字符)
			res.Err = res.Err[0:limit]
		}
	} else {
		log.Infow("Task done success", "task-id", res.TaskID)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case rres <- res:
		return nil
	}
}

// export for rpc service
func (sb *Sealer) WorkerFileWatch(ctx context.Context, res WorkerCfg) error {

	remotesInter, ok := _remotes.Load(res.ID)
	if !ok {
		return errors.New("not_found_worker_" + res.ID)
	}
	remote1 := remotesInter.(*remote)
	log.Info("=====================before=================", remote1.cfg.MaxTaskNum, "===after=====", res.MaxTaskNum, "======", remote1.cfg.ID, "=====", remote1.cfg.SvcUri)
	var workerCfg = WorkerCfg{
		ID:             remote1.cfg.ID,
		IP:             remote1.cfg.IP,
		SvcUri:         remote1.cfg.SvcUri,
		Cycle:          remote1.cfg.Cycle,
		Retry:          remote1.cfg.Retry,
		Busy:           remote1.cfg.Busy,
		C2Sids:         remote1.cfg.C2Sids,
		CacheMode:      remote1.cfg.CacheMode,
		TransferBuffer: remote1.cfg.TransferBuffer,

		MaxTaskNum:         res.MaxTaskNum,
		ParallelPledge:     res.ParallelPledge,
		ParallelPrecommit1: res.ParallelPrecommit1,
		ParallelPrecommit2: res.ParallelPrecommit2,
		ParallelCommit:     res.ParallelCommit,
		Commit2Srv:         res.Commit2Srv,
		WdPoStSrv:          res.WdPoStSrv,
		WnPoStSrv:          res.WnPoStSrv,
	}

	remote1.cfg = workerCfg
	_remotes.Store(workerCfg.ID, remote1)
	return nil
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
func (sb *Sealer) GetWorkerBusyTask(ctx context.Context, wid string) (int, error) {
	tasks, err := sb.TaskWorking(wid)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}
func (sb *Sealer) RequestDisableWorker(ctx context.Context, wid string) error {
	return sb.DisableWorker(ctx, wid, true)
}
