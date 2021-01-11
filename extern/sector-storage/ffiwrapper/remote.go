package ffiwrapper

import (
	"context"
	"encoding/json"
	"fmt"
	buriedmodel "github.com/filecoin-project/lotus/buried/model"
	"github.com/filecoin-project/lotus/lib/report"
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

func (sb *Sealer) PledgeSector(ctx context.Context, sector storage.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, sizes ...abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	log.Infof("DEBUG:PledgeSector in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:PledgeSector out,%+v", sector)
	if len(sizes) == 0 {
		log.Info("No sizes for pledge")
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

	log.Infof("DEBUG:PledgeSector prefer remote,%+v", sector)
	select { // prefer remote
	case _pledgeTasks <- call:
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

	// if the FIL_PROOFS_MULTICORE_SDR_PRODUCERS haven't set, set it by auto.
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
		if ret.Err != "" {
			return storage.SectorCids{}, errors.Parse(ret.Err)
		}
		return ret.PreCommit2Out, nil
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

func (sb *Sealer) sealCommitRemote(call workerCall) (storage.Proof, error) {
	log.Infof("DEBUG:sealCommitRemote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:sealCommitRemote out,%+v", call.task.SectorID)

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
	log.Infof("DEBUG:SealCommit in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:SealCommit out,%+v", sector)
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
	log.Infof("DEBUG:SealCommit prefer remote,%+v", sector)
	// send to remote worker
	select {
	case _commitTasks <- call:
		log.Infof("DEBUG:SealCommit prefer remote called,%+v", sector)
		return sb.sealCommitRemote(call)
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

func (sb *Sealer) unsealPieceRemote(call workerCall) error {
	log.Infof("DEBUG:unsealPieceRemote in,%+v", call.task.SectorID)
	defer log.Infof("DEBUG:unsealPieceRemote out,%+v", call.task.SectorID)
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
	log.Infof("DEBUG:UnsealPiece in(remote:%t),%+v", sb.remoteCfg.SealSector, sector)
	defer log.Infof("DEBUG:UnsealPiece out,%+v", sector)

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
	log.Infof("DEBUG:UnsealPiece prefer remote,%+v", sector)
	select { // prefer remote
	case _unsealTasks <- call:
		log.Infof("DEBUG:UnsealPiece prefer remote called,%+v", sector)
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

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*1e9)
	defer cancel()

	result := make(chan struct {
		proofs []proof.PoStProof
		err    error
	}, 1)
	go func() {
		p, err := sb.generateWinningPoStWithTimeout(timeoutCtx, minerID, sectorInfo, randomness)
		result <- struct {
			proofs []proof.PoStProof
			err    error
		}{p, err}
	}()
	select {
	case p := <-result:
		return p.proofs, p.err
	case <-ctx.Done():
		return []proof.PoStProof{}, errors.New("Generate winning post timeout")
	}
}
func (sb *Sealer) generateWinningPoStWithTimeout(ctx context.Context, minerID abi.ActorID, sectorInfo []storage.ProofSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	// remote worker haven't set.
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
		log.Infof("Selected GpuService:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		log.Info("No GpuService Found, using local mode")
		return sb.generateWinningPoSt(ctx, minerID, sectorInfo, randomness)
	}

	type resp struct {
		res       SealRes
		interrupt bool
	}
	result := make(chan resp, len(remotes))

	for _, r := range remotes {
		go func(req *req) {
			defer sb.UnlockGPUService(ctx, req.remote.cfg.ID, req.task.Key())

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

	// remote worker haven't set.
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
		log.Infof("Selected GpuService:%s", r.cfg.SvcUri)
	}
	if len(remotes) == 0 {
		// using the old version when EnableForceRemoteWindowPoSt is not set.

		if !sb.remoteCfg.EnableForceRemoteWindowPoSt {
			log.Info("No GpuService count needed, using local mode")
			return sb.generateWindowPoSt(ctx, minerID, sectorInfo, randomness)
		}

		retrycount++
		if retrycount < 60 {
			log.Warnf(" retry select gpuservice:%d", retrycount)
			time.Sleep(10 * time.Second)
			goto selectWorker
		}

		log.Error("timeout for select gpuservice, no gpu service found")
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

// Need call sb.UnlockService to release this selected.
// if no commit2 service, it will block the function call.
// TODO: auto unlock service when deadlock happen.
func (sb *Sealer) SelectCommit2Service(ctx context.Context, sector abi.SectorID) (*WorkerCfg, error) {
	log.Infof("SelectCommit2Service in:s-t%d-%d", sector.Miner, sector.Number)
	selectCommit2ServiceLock.Lock()
	handler := func(r *remote) {
		endTime := time.Now().Unix()
		minerId := "s-t0" + sector.Miner.String()
		sectorId := minerId + "-" + sector.Number.String()
		log.Infof("Report sector in:%v", sectorId)
		err := CollectSectorC2StateInfo(endTime, minerId, sectorId, r.cfg)
		if err != nil {
			log.Error("Sector-Report Err,SectorId:%d", sector.Number, err)
		}
	}
	defer func() {
		log.Infof("SelectCommit2Service out:s-t%d-%d", sector.Miner, sector.Number)
		selectCommit2ServiceLock.Unlock()
	}()

	task := WorkerTask{
		Type:     WorkerCommit,
		SectorID: sector,
	}
	sid := task.SectorName()

	tick := time.Tick(3e9)
	checking := make(chan bool, 1)
	checking <- true // not wait for the first request.
	defer func() {
		close(checking)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("user canceled").As(sid)
		case <-tick:
			checking <- true
		case <-checking:
			r, ok := sb.selectGPUService(ctx, sid, task)
			if !ok {
				continue
			}
			handler(r)
			return &r.cfg, nil
		}
	}
	return nil, errors.New("not reach here").As(sid)
}

func CollectSectorC2StateInfo(endTime int64, minerId string, sectorId string, workercfg WorkerCfg) error {
	sectorStateInfo := &buriedmodel.SectorState{
		MinerID:  minerId,
		WorkerID: workercfg.ID,
		ClientIP: workercfg.IP,
		//		SectorSize: task.SectorStorage.StorageInfo.SectorSize,
		// SectorID: storage.SectorName(m.minerSectorID(state.SectorNumber)),
		SectorID:   sectorId,
		State:      "Commit2WaitDone",
		CreateTime: endTime,
		StatusType: "02",
	}
	sectorsDataBytes, err := json.Marshal(sectorStateInfo)

	if err != nil {
		return err
	}
	reqData := &buriedmodel.BuriedDataCollectParams{
		DataType: "sector_state",
		Data:     sectorsDataBytes,
	}
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		log.Error(err)
	}

	report.SendReport(reqDataBytes)

	sectorStateInfo.CreateTime = endTime
	sectorStateInfo.State = "Commit2Start"
	sectorsDataBytes, err = json.Marshal(sectorStateInfo)

	if err != nil {
		return err
	}
	reqData = &buriedmodel.BuriedDataCollectParams{
		DataType: "sector_state",
		Data:     sectorsDataBytes,
	}
	reqDataBytes, err = json.Marshal(reqData)
	if err != nil {
		log.Error(err)
	}
	report.SendReport(reqDataBytes)
	return nil
}
