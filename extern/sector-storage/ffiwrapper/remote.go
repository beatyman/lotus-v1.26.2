package ffiwrapper

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

var (
	ErrTaskCancel = errors.New("task cancel")
	ErrNoGpuSrv   = errors.New("No Gpu service for allocation")
)

var (
	_pledgeTasks     = make(chan workerCall)
	_precommit1Tasks = make(chan workerCall)
	_precommit2Tasks = make(chan workerCall)
	_commitTasks     = make(chan workerCall)
	_finalizeTasks   = make(chan workerCall)
	_unsealTasks     = make(chan workerCall)

	_remotes        = sync.Map{}
	_remoteResultLk = sync.RWMutex{}
	_remoteResult   = make(map[string]chan<- SealRes)
	_remoteGpuLk    = sync.Mutex{}

	// if set, should call back the task consume event with goroutine.
	_pledgeListenerLk = sync.Mutex{}
	_pledgeListener   func(WorkerTask)

	_pledgeWait     int32
	_precommit1Wait int32
	_precommit2Wait int32
	_commitWait     int32
	_finalizeWait   int32
	_unsealWait     int32

	sourceId = int64(1000000000) // the sealed sector need less than this value
	sourceLk = sync.Mutex{}
)

func nextSourceID() int64 {
	sourceLk.Lock()
	defer sourceLk.Unlock()
	sourceId++
	if sourceId == math.MaxInt64 {
		sourceId = 1000000000
	}
	return sourceId
}

func (sb *Sealer) pledgeRemote(call workerCall) ([]abi.PieceInfo, error) {
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

func (sb *Sealer) PledgeSector(ctx context.Context, sector storage.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, sizes ...abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	log.Infof("DEBUG:PledgeSector in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:PledgeSector out,%+v", sector.ID)
	if len(sizes) == 0 {
		log.Infof("No sizes for pledge %+v", sector.ID)
		return nil, nil
	}

	atomic.AddInt32(&_pledgeWait, 1)
	if !sb.remoteCfg.SealSector {
		return sb.pledgeSector(ctx, sector, existingPieceSizes, sizes...)
	}

	call := workerCall{
		// no need worker id
		task: WorkerTask{
			Type:               WorkerPledge,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			ExistingPieceSizes: existingPieceSizes,
			ExtSizes:           sizes,
		},
		ret: make(chan SealRes),
	}

	select { // prefer remote
	case _pledgeTasks <- call:
		return sb.pledgeRemote(call)
	}
}

func (sb *Sealer) sealPreCommit1Remote(call workerCall) (storage.PreCommit1Out, error) {
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
	log.Infof("DEBUG:SealPreCommit1 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:SealPreCommit1 out,%+v", sector.ID)

	// if the FIL_PROOFS_MULTICORE_SDR_PRODUCERS is not set, set it by auto.
	if len(os.Getenv("FIL_PROOFS_MULTICORE_SDR_PRODUCERS")) == 0 {
		if err := autoPrecommit1Env(ctx); err != nil {
			return storage.PreCommit1Out{}, errors.As(err)
		}
	}

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
			Pieces:     pieces,
		},
		ret: make(chan SealRes),
	}
	select { // prefer remote
	case _precommit1Tasks <- call:
		return sb.sealPreCommit1Remote(call)
	case <-ctx.Done():
		return storage.PreCommit1Out{}, ctx.Err()
	}
}

func (sb *Sealer) sealPreCommit2Remote(call workerCall) (storage.SectorCids, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return storage.SectorCids{}, errors.Parse(ret.Err)
		}
		return ret.PreCommit2Out, nil
	case <-sb.stopping:
		return storage.SectorCids{}, xerrors.New("sectorbuilder stopped")
	}
}
func (sb *Sealer) SealPreCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage.PreCommit1Out) (storage.SectorCids, error) {
	log.Infof("DEBUG:SealPreCommit2 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:SealPreCommit2 out,%+v", sector.ID)

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

	select { // prefer remote
	case _precommit2Tasks <- call:
		return sb.sealPreCommit2Remote(call)
	case <-ctx.Done():
		return storage.SectorCids{}, ctx.Err()
	}
}

func (sb *Sealer) sealCommitRemote(call workerCall) (storage.Proof, error) {
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

func (sb *Sealer) SealCommit(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage.SectorCids) (storage.Proof, error) {
	log.Infof("DEBUG:SealCommit in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:SealCommit out,%+v", sector.ID)
	atomic.AddInt32(&_commitWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_commitWait, -1)
		return storage.Proof{}, errors.New("No SealCommit for local mode.")
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerCommit,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			SealTicket: ticket,
			Pieces:     pieces,

			SealSeed: seed,
			Cids:     cids,
		},
		ret: make(chan SealRes),
	}
	// send to remote worker
	select {
	case _commitTasks <- call:
		return sb.sealCommitRemote(call)
	case <-ctx.Done():
		return storage.Proof{}, ctx.Err()
	}
}

func (sb *Sealer) finalizeSectorRemote(call workerCall) error {
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
	log.Infof("DEBUG:FinalizeSector in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:FinalizeSector out,%+v", sector.ID)
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

	// send to remote worker
	select {
	case _finalizeTasks <- call:
		return sb.finalizeSectorRemote(call)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sb *Sealer) unsealPieceRemote(call workerCall) error {
	select {
	case ret := <-call.ret:
		var err error
		if ret.Err != "" {
			err = xerrors.New(ret.Err)
		}
		return err
	case <-sb.stopping:
		return xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) UnsealPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, commd cid.Cid) error {
	log.Infof("DEBUG:UnsealPiece in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:UnsealPiece out,%+v", sector.ID)

	// TODO: unseal with concurrency
	// TODO: make global lock
	unsealKey := fmt.Sprintf("unsealing-%s", sectorName(sector.ID))
	_, exist := sb.unsealing.LoadOrStore(unsealKey, true)
	if exist {
		return errors.New("the sector is unsealing").As(sectorName(sector.ID))
	}
	defer sb.unsealing.Delete(unsealKey)

	if len(os.Getenv("FIL_PROOFS_MULTICORE_SDR_PRODUCERS")) == 0 {
		if err := autoPrecommit1Env(ctx); err != nil {
			return errors.As(err)
		}
	}

	atomic.AddInt32(&_unsealWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_unsealWait, -1)
		return sb.unsealPiece(ctx, sector, offset, size, randomness, commd)
	}

	call := workerCall{
		task: WorkerTask{
			Type:      WorkerUnseal,
			ProofType: sector.ProofType,
			SectorID:  sector.ID,

			UnsealOffset:   offset,
			UnsealSize:     size,
			SealRandomness: randomness,
			Commd:          commd,
		},
		ret: make(chan SealRes),
	}
	select { // prefer remote
	case _unsealTasks <- call:
		return sb.unsealPieceRemote(call)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sb *Sealer) GenerateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	if len(sectorInfo) == 0 {
		return nil, errors.New("not sectors set")
	}
	sessionKey := uuid.New().String()
	log.Infof("DEBUG:GenerateWinningPoSt in(remote:%t),%s, session:%s", sb.remoteCfg.SealSector, minerID, sessionKey)
	defer log.Infof("DEBUG:GenerateWinningPoSt out,%s, session:%s", minerID, sessionKey)
	return sb.generateWinningPoStWithTimeout(ctx, minerID, sectorInfo, randomness)
}
func (sb *Sealer) generateWinningPoStWithTimeout(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	// remote worker is not set, use local mode
	if sb.remoteCfg.WinningPoSt == 0 {
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
			ProofType:  sectorInfo[0].ProofType,
			SectorID:   abi.SectorID{Miner: minerID, Number: abi.SectorNumber(nextSourceID())}, // unique task.Key()
			SectorInfo: sectorInfo,
			Randomness: randomness,
		}
		sid := task.SectorName()

		r, ok := sb.selectGPUService(ctx, sid, task)
		if !ok {
			continue
		}
		remotes = append(remotes, &req{r, &task})
		log.Infof("Selected GpuService for winning PoSt:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		log.Info("No GpuService for winning PoSt, using local mode")
		return sb.generateWinningPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type resp struct {
		res       SealRes
		interrupt bool
	}
	result := make(chan resp, len(remotes))

	for _, r := range remotes {
		go func(req *req) {
			defer sb.UnlockGPUService(ctx, &Commit2Result{WorkerId: req.remote.cfg.ID, TaskKey: req.task.Key()})

			// send to remote worker
			res, interrupt := sb.TaskSend(ctx, req.remote, *req.task)
			result <- resp{res, interrupt}
		}(r)
	}

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
		}
	}
	return res.res.WinningPoStProofOut, err
}

func (sb *Sealer) GenerateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, []abi.SectorID, error) {
	if len(sectorInfo) == 0 {
		return nil, nil, errors.New("not sectors set")
	}
	sessionKey := uuid.New().String()
	log.Infof("DEBUG:GenerateWindowPoSt in(remote:%t,%t),%s,session:%s", sb.remoteCfg.SealSector, sb.remoteCfg.EnableForceRemoteWindowPoSt, minerID, sessionKey)
	defer log.Infof("DEBUG:GenerateWindowPoSt out,%s,session:%s", minerID, sessionKey)

	// remote worker is not set, use local mode
	if sb.remoteCfg.WindowPoSt == 0 {
		return sb.generateWindowPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type req = struct {
		remote *remote
		task   *WorkerTask
	}
	remotes := []*req{}
	var retrycount int = 0
selectWorker:
	for i := 0; i < sb.remoteCfg.WindowPoSt; i++ {
		task := WorkerTask{
			Type:       WorkerWindowPoSt,
			ProofType:  sectorInfo[0].ProofType,
			SectorID:   abi.SectorID{Miner: minerID, Number: abi.SectorNumber(nextSourceID())}, // unique task.Key()
			SectorInfo: sectorInfo,
			Randomness: randomness,
		}
		sid := task.SectorName()
		r, ok := sb.selectGPUService(ctx, sid, task)
		if !ok {
			continue
		}
		remotes = append(remotes, &req{r, &task})
		log.Infof("Selected GpuService for window PoSt:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		// using the old version when EnableForceRemoteWindowPoSt is not set.

		if !sb.remoteCfg.EnableForceRemoteWindowPoSt {
			log.Info("No GpuService for window PoSt, using local mode")
			return sb.generateWindowPoSt(ctx, minerID, sectorInfo, randomness)
		}

		retrycount++
		if retrycount < 60 {
			log.Warnf(" retry select gpuservice for window PoSt, times:%d", retrycount)
			time.Sleep(10 * time.Second)
			goto selectWorker
		}

		log.Error("timeout for select gpuservice, no gpu service for window PoSt")
		return nil, nil, errors.New("timeout for select gpuservice,no gpu service found")
	}

	type resp struct {
		res       SealRes
		interrupt bool
	}
	result := make(chan resp, len(remotes))
	for _, r := range remotes {
		go func(req *req) {
			ctx := context.TODO()
			defer sb.UnlockGPUService(ctx, &Commit2Result{WorkerId: req.remote.cfg.ID, TaskKey: req.task.Key()})

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

// Need call sb.UnlockService to release this selected.
// if no commit2 service, it will block the function call.
func (sb *Sealer) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*Commit2Worker, error) {
	task := WorkerTask{
		Type:     WorkerCommit,
		SectorID: sector,
	}
	sid := task.SectorName()

	//1.优先从缓存获取
	if wid, prf, ok := c2cache.get(sid); ok {
		return &Commit2Worker{
			WorkerId: wid,
			Proof:    prf,
		}, nil
	}
	//2.其次选择worker
	if r, ok := sb.selectGPUService(ctx, sid, task); ok {
		return &Commit2Worker{
			WorkerId: r.cfg.ID,
			Url:      r.cfg.SvcUri,
		}, nil
	}
	return nil, errors.New("idle gpu not found")
}
