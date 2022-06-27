package ffiwrapper

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"
	"go.opencensus.io/trace/propagation"
	"golang.org/x/xerrors"
	"sync/atomic"
)

func (sb *Sealer) replicaUpdateRemote(call workerCall) (storage.ReplicaUpdateOut, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return storage.ReplicaUpdateOut{}, errors.Parse(ret.Err)
		}
		return ret.ReplicaUpdateOut, nil
	case <-sb.stopping:
		return storage.ReplicaUpdateOut{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) ReplicaUpdate(ctx context.Context, sector storage.SectorRef, pieces []abi.PieceInfo) (storage.ReplicaUpdateOut, error) {
	log.Infof("DEBUG:ReplicaUpdate in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:ReplicaUpdate out,%+v", sector.ID)

	atomic.AddInt32(&_precommit2Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_precommit2Wait, -1)
		return sb.replicaUpdate(ctx, sector, pieces)
	}

	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerPreCommit2,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			SectorRepairStatus: sector.SectorRepairStatus,

			Pieces: pieces,
		},
		ret: make(chan SealRes),
	}

	select { // prefer remote
	case _precommit2Tasks <- call:
		return sb.replicaUpdateRemote(call)
	case <-ctx.Done():
		return storage.ReplicaUpdateOut{}, ctx.Err()
	}
}

func (sb *Sealer) proveReplicaUpdateRemote(call workerCall) (storage.ReplicaUpdateProof, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return storage.ReplicaUpdateProof{}, errors.Parse(ret.Err)
		}
		return ret.ProveReplicaUpdateOut, nil
	case <-sb.stopping:
		return storage.ReplicaUpdateProof{}, xerrors.New("sectorbuilder stopped")
	}
}

func (m *Sealer) ProveReplicaUpdate(ctx context.Context, sector storage.SectorRef, sectorKey, newSealed, newUnsealed cid.Cid) (storage.ReplicaUpdateProof, error) {
	log.Infof("DEBUG:ProveReplicaUpdate in(remote:%t),%+v", m.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:ProveReplicaUpdate out,%+v", sector.ID)
	atomic.AddInt32(&_commitWait, 1)
	if !m.remoteCfg.SealSector {
		atomic.AddInt32(&_commitWait, -1)
		return storage.ReplicaUpdateProof{}, errors.New("No ProveReplicaUpdate for local mode.")
	}

	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerCommit,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			SectorRepairStatus: sector.SectorRepairStatus,

			SectorKey:   sectorKey,
			NewSealed:   newSealed,
			NewUnsealed: newUnsealed,
		},
		ret: make(chan SealRes),
	}
	// send to remote worker
	select {
	case _commitTasks <- call:
		return m.proveReplicaUpdateRemote(call)
	case <-ctx.Done():
		return storage.ReplicaUpdateProof{}, ctx.Err()
	}
}

func (sb *Sealer) finalizeReplicaUpdateRemote(call workerCall) error {
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

func (sb *Sealer) FinalizeReplicaUpdate(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage.Range) error {
	log.Infof("DEBUG:FinalizeReplicaUpdate in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:FinalizeReplicaUpdate out,%+v", sector.ID)
	// return sb.finalizeSector(ctx, sector)

	atomic.AddInt32(&_finalizeWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_finalizeWait, -1)
		return sb.finalizeReplicaUpdate(ctx, sector, keepUnsealed)
	}

	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerFinalize,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			SectorRepairStatus: sector.SectorRepairStatus,
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