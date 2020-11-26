package ffiwrapper

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"golang.org/x/xerrors"
)

var (
	ErrTaskCancel = errors.New("task cancel")
)

var (
	_addPieceTasks   = make(chan workerCall)
	_precommit1Tasks = make(chan workerCall)
	_precommit2Tasks = make(chan workerCall)
	_commit1Tasks    = make(chan workerCall)
	_commit2Tasks    = make(chan workerCall)
	_finalizeTasks   = make(chan workerCall)

	_remoteMarket   = sync.Map{}
	_remotes        = sync.Map{}
	_remoteResultLk = sync.RWMutex{}
	_remoteResult   = make(map[string]chan<- SealRes)
	_remoteGpuLk    = sync.Mutex{}

	// if set, should call back the task consume event with goroutine.
	_addPieceListenerLk = sync.Mutex{}
	_addPieceListener   func(WorkerTask)

	_addPieceWait   int32
	_precommit1Wait int32
	_precommit2Wait int32
	_commit1Wait    int32
	_commit2Wait    int32
	_unsealWait     int32
	_finalizeWait   int32

	sourceId = time.Now().UnixNano()
	sourceLk = sync.Mutex{}
)

func curSourceID() int64 {
	sourceLk.Lock()
	defer sourceLk.Unlock()
	return sourceId
}

func nextSourceID() int64 {
	sourceLk.Lock()
	defer sourceLk.Unlock()
	sourceId++
	return sourceId
}

func hasMarketRemotes() bool {
	has := false
	_remoteMarket.Range(func(key, val interface{}) bool {
		r := val.(*remote)
		r.lock.Lock()
		if r.disable || len(r.busyOnTasks) >= r.cfg.MaxTaskNum {
			r.lock.Unlock()
			// continue range
			return true
		}
		r.lock.Unlock()

		has = true
		// break range
		return false
	})

	return has
}

func (sb *Sealer) pledgeRemote(call workerCall) ([]abi.PieceInfo, error) {
	sectorID := call.task.SectorID
	log.Infof("DEBUG:pledgeRemote in,%+v", sectorID)
	defer log.Infof("DEBUG:pledgeRemote out,%+v", sectorID)

	select {
	case ret := <-call.ret:
		var err error
		if ret.Err != "" {
			err = xerrors.New(ret.Err)
		}
		return ret.Pieces, err
	case <-sb.stopping:
		return []abi.PieceInfo{}, xerrors.New("sectorbuilder stopped")
	}
}
func (sb *Sealer) PledgeSector(sector storage.SectorRef, pieceSize []abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	log.Infof("DEBUG:PledgeSector in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:PledgeSector out,%+v", sector)
	atomic.AddInt32(&_addPieceWait, 1)
	if !sb.remoteCfg.SealSector {
		panic("no local mode for pledge sector")
	}

	call := workerCall{
		// no need worker id
		task: WorkerTask{
			Type:       WorkerAddPiece,
			ProofType:  sector.ProofType,
			SectorID:   sector.ID,
			PieceSizes: pieceSize,
		},
		ret: make(chan SealRes),
	}

	log.Infof("DEBUG:PledgeSector prefer remote,%+v", sector)
	select { // prefer remote
	case _addPieceTasks <- call:
		log.Infof("DEBUG:PledgeSector prefer remote called,%+v", sector)
		return sb.pledgeRemote(call)
	}
}

func (sb *Sealer) sealPreCommit1Remote(call workerCall) (storage.PreCommit1Out, error) {
	log.Infof("DEBUG:sealPreCommit1Remote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:sealPreCommit1Remote out,%+v", call.task.SectorID)
	select {
	case ret := <-call.ret:
		var err error
		if ret.Err != "" {
			err = xerrors.New(ret.Err)
		}
		return ret.PreCommit1Out, err
	case <-sb.stopping:
		return storage.PreCommit1Out{}, xerrors.New("sectorbuilder stopped")
	}
}
func (sb *Sealer) SealPreCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, pieces []abi.PieceInfo) (out storage.PreCommit1Out, err error) {
	log.Infof("DEBUG:SealPreCommit1 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:SealPreCommit1 out,%+v", sector)
	atomic.AddInt32(&_precommit1Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_precommit1Wait, -1)
		return sb.sealPreCommit1(ctx, sector, ticket, pieces)
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerPreCommit1,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			SealTicket: ticket,
			Pieces:     EncodePieceInfo(pieces),
		},
		ret: make(chan SealRes),
	}
	log.Infof("DEBUG:SealPreCommit1 prefer remote,%+v", sector)
	select { // prefer remote
	case _precommit1Tasks <- call:
		log.Infof("DEBUG:SealPreCommit1 prefer remote called,%+v", sector)
		return sb.sealPreCommit1Remote(call)
	case <-ctx.Done():
		return storage.PreCommit1Out{}, ctx.Err()
	}
}

func (sb *Sealer) sealPreCommit2Remote(call workerCall) (storage.SectorCids, error) {
	log.Infof("DEBUG:sealPreCommit2Remote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:sealPreCommit2Remote out,%+v", call.task.SectorID)
	select {
	case ret := <-call.ret:
		var err error
		if ret.Err != "" {
			return storage.SectorCids{}, errors.Parse(ret.Err)
		}
		out, err := ret.PreCommit2Out.Decode()
		if err != nil {
			return storage.SectorCids{}, errors.As(err)
		}
		return *out, nil
	case <-sb.stopping:
		return storage.SectorCids{}, xerrors.New("sectorbuilder stopped")
	}
}
func (sb *Sealer) SealPreCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage.PreCommit1Out) (storage.SectorCids, error) {
	log.Infof("DEBUG:SealPreCommit2 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:SealPreCommit2 out,%+v", sector)

	atomic.AddInt32(&_precommit2Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_precommit2Wait, -1)
		return sb.sealPreCommit2(ctx, sector, phase1Out)
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerPreCommit2,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			PreCommit1Out: phase1Out,
		},
		ret: make(chan SealRes),
	}

	log.Infof("DEBUG:SealPreCommit2 prefer remote,%+v", sector)
	select { // prefer remote
	case _precommit2Tasks <- call:
		log.Infof("DEBUG:SealPreCommit2 prefer remote called,%+v", sector)
		return sb.sealPreCommit2Remote(call)
	case <-ctx.Done():
		return storage.SectorCids{}, ctx.Err()
	}
}

func (sb *Sealer) sealCommit1Remote(call workerCall) (storage.Commit1Out, error) {
	log.Infof("DEBUG:sealCommit1Remote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:sealCommit1Remote out,%+v", call.task.SectorID)

	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return ret.Commit1Out, xerrors.New(ret.Err)
		}
		return ret.Commit1Out, nil
	case <-sb.stopping:
		return storage.Commit1Out{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) SealCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage.SectorCids) (storage.Commit1Out, error) {
	log.Infof("DEBUG:SealCommit1 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:SealCommit1 out,%+v", sector)
	atomic.AddInt32(&_commit1Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_commit1Wait, -1)
		return sb.sealCommit1(ctx, sector, ticket, seed, pieces, cids)
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerCommit1,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			SealTicket: ticket,
			Pieces:     EncodePieceInfo(pieces),

			SealSeed: seed,
			Cids: SectorCids{
				Unsealed: cids.Unsealed.String(),
				Sealed:   cids.Sealed.String(),
			},
		},
		ret: make(chan SealRes),
	}
	log.Infof("DEBUG:SealCommit1 prefer remote,%+v", sector)
	// send to remote worker
	select {
	case _commit1Tasks <- call:
		log.Infof("DEBUG:SealCommit1 prefer remote called,%+v", sector)
		return sb.sealCommit1Remote(call)
	case <-ctx.Done():
		return storage.Commit1Out{}, ctx.Err()
	}
}

func (sb *Sealer) sealCommit2Remote(call workerCall) (storage.Proof, error) {
	log.Infof("DEBUG:sealCommit2Remote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:sealCommit2Remote out,%+v", call.task.SectorID)

	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return ret.Commit2Out, xerrors.New(ret.Err)
		}
		return ret.Commit2Out, nil
	case <-sb.stopping:
		return storage.Proof{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) SealCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage.Commit1Out) (storage.Proof, error) {
	log.Infof("DEBUG:SealCommit2 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:SealCommit2 out,%+v", sector)

	atomic.AddInt32(&_commit2Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_commit2Wait, -1)
		return sb.sealCommit2(ctx, sector, phase1Out)
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerCommit2,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			Commit1Out: phase1Out,
		},
		ret: make(chan SealRes),
	}

	log.Infof("DEBUG:SealCommit2 prefer remote,%+v", sector)
	// send to remote worker
	select {
	case _commit2Tasks <- call:
		log.Infof("DEBUG:SealCommit2 prefer remote called,%+v", sector)
		return sb.sealCommit2Remote(call)
	case <-ctx.Done():
		return storage.Proof{}, ctx.Err()
	}
}

func (sb *Sealer) finalizeSectorRemote(call workerCall) error {
	log.Infof("DEBUG:finalizeSectorRemote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:finalizeSectorRemote out,%+v", call.task.SectorID)

	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return xerrors.New(ret.Err)
		}
		return nil
	case <-sb.stopping:
		return xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) FinalizeSector(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage.Range) error {
	log.Infof("DEBUG:FinalizeSector in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:FinalizeSector out,%+v", sector)
	// return sb.finalizeSector(ctx, sector)

	atomic.AddInt32(&_finalizeWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_finalizeWait, -1)
		return sb.finalizeSector(ctx, sector, keepUnsealed)
	}
	// close finalize because it has done in commit2
	//atomic.AddInt32(&_finalizeWait, -1)
	//return nil

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerFinalize,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,
		},
		ret: make(chan SealRes),
	}

	log.Infof("DEBUG:FinalizeSector prefer remote,%+v", sector)
	// send to remote worker
	select {
	case _finalizeTasks <- call:
		log.Infof("DEBUG:FinalizeSector prefer remote called,%+v", sector)
		return sb.finalizeSectorRemote(call)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sb *Sealer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	log.Infof("DEBUG:GenerateWiningPoSt in(remote:%t),%+v", sb.remoteCfg.SealSector, minerID)
	defer log.Infof("DEBUG:GenerateWinningPoSt out,%+v", minerID)
	if sb.remoteCfg.WinningPoSt < 1 {
		// TODO: when the storage broken, it will be block here.
		// TODO: make rust with a timeout mechanism
		return sb.generateWinningPoSt(ctx, minerID, sectorInfo, randomness)
	}
	type req = struct {
		remote *remote
		task   *WorkerTask
	}
	remotes := []*req{}
	for i := 0; i < sb.remoteCfg.WinningPoSt; i++ {
		task := WorkerTask{
			Type:       WorkerWinningPoSt,
			SectorID:   abi.SectorID{Miner: minerID, Number: abi.SectorNumber(nextSourceID())}, // unique task.Key()
			SectorInfo: sectorInfo,
			Randomness: randomness,
		}
		sid := task.GetSectorID()

		r, err := sb.selectGPUService(ctx, sid, task)
		if err != nil {
			continue
		}
		remotes = append(remotes, &req{r, &task})
		log.Infof("Selected GpuService:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		log.Info("No GpuServie Found, using local mode")
		return sb.generateWinningPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type resp struct {
		res       SealRes
		interrupt bool
	}
	result := make(chan resp, len(remotes))

	workerCtx, workerCancel := context.WithCancel(ctx)
	for _, r := range remotes {
		go func(req *req) {
			defer sb.UnlockGPUService(workerCtx, req.remote.cfg.ID, req.task.Key())

			// send to remote worker
			res, interrupt := sb.TaskSend(workerCtx, req.remote, *req.task)
			result <- resp{res, interrupt}
		}(r)
	}

	timeout := time.After(30e9)
	var err error
	var res resp
	for i := len(remotes); i > 0; i-- {
		select {
		case res := <-result:
			if res.interrupt {
				err = ErrTaskCancel.As(minerID)
				continue
			}
			if res.res.GoErr != nil {
				err = errors.As(res.res.GoErr)
				continue
			}
			if len(res.res.Err) > 0 {
				err = errors.New(res.res.Err)
				continue
			}
			// only select the fastest success result to return
			return res.res.WinningPoStProofOut, nil
		case <-ctx.Done():
			return nil, errors.New("cancel winning post")
		case <-timeout:
			workerCancel()
			return nil, errors.New("worker timeout")
		}
	}
	return res.res.WinningPoStProofOut, err
}

func (sb *Sealer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, []abi.SectorID, error) {
	curSourceId := curSourceID()
	log.Infof("DEBUG:GenerateWindowPoSt in(remote:%t),%s-%d", sb.remoteCfg.SealSector, minerID, curSourceId)
	defer log.Infof("DEBUG:GenerateWindowPoSt out,%s-%d", minerID, curSourceId)

	if sb.remoteCfg.WindowPoSt < 1 {
		return sb.generateWindowPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type req = struct {
		remote *remote
		task   *WorkerTask
	}
	remotes := []*req{}
	for i := 0; i < sb.remoteCfg.WindowPoSt; i++ {
		task := WorkerTask{
			Type:       WorkerWindowPoSt,
			SectorID:   abi.SectorID{Miner: minerID, Number: abi.SectorNumber(nextSourceID())}, // unique task.Key()
			SectorInfo: sectorInfo,
			Randomness: randomness,
		}
		sid := task.GetSectorID()
		r, err := sb.selectGPUService(ctx, sid, task)
		if err != nil {
			continue
		}
		remotes = append(remotes, &req{r, &task})
		log.Infof("Selected GpuService:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		log.Info("No GpuServie Found, using local mode")
		return sb.generateWindowPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type resp struct {
		res       SealRes
		interrupt bool
	}
	result := make(chan resp, len(remotes))
	for _, r := range remotes {
		go func(req *req) {
			ctx := context.TODO()
			defer sb.UnlockGPUService(ctx, req.remote.cfg.ID, req.task.Key())

			// send to remote worker
			res, interrupt := sb.TaskSend(ctx, req.remote, *req.task)
			result <- resp{res, interrupt}
		}(r)
	}

	var err error
	var res resp
	for i := len(remotes); i > 0; i-- {
		res = <-result
		if res.interrupt {
			err = ErrTaskCancel.As(minerID)
			continue
		}
		if res.res.GoErr != nil {
			err = errors.As(res.res.GoErr)
		} else if len(res.res.Err) > 0 {
			err = errors.New(res.res.Err)
		}

		if len(res.res.WindowPoStIgnSectors) > 0 {
			// when ignore is defined, return the ignore and do the next.
			return res.res.WindowPoStProofOut, res.res.WindowPoStIgnSectors, err
		}
		if err != nil {
			// continue to sector a correct result.
			continue
		}

		// only select the fastest success result to return
		return res.res.WindowPoStProofOut, res.res.WindowPoStIgnSectors, err
	}
	return res.res.WindowPoStProofOut, res.res.WindowPoStIgnSectors, err
}

var selectCommit2ServiceLock = sync.Mutex{}

// Need call sb.UnlockService to release the selected.
func (sb *Sealer) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*WorkerCfg, error) {
	selectCommit2ServiceLock.Lock()
	defer selectCommit2ServiceLock.Unlock()

	task := WorkerTask{
		Type:     WorkerCommit2,
		SectorID: sector,
	}
	sid := task.GetSectorID()
	r, err := sb.selectGPUService(ctx, sid, task)
	if err != nil {
		return nil, errors.As(err)
	}
	return &r.cfg, nil
}
