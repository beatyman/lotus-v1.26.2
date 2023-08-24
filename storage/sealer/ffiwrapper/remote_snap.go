package ffiwrapper

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/ipfs/go-cid"
	"go.opencensus.io/trace/propagation"
	"golang.org/x/xerrors"
	"sync/atomic"
)

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

func (sb *Sealer) FinalizeReplicaUpdate(ctx context.Context, sector storiface.SectorRef) error {
	log.Infof("DEBUG:FinalizeReplicaUpdate in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:FinalizeReplicaUpdate out,%+v", sector.ID)
	// uprade SectorRef
	atomic.AddInt32(&_finalizeReplicaUpdateWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_finalizeReplicaUpdateWait, -1)
		return sb.finalizeReplicaUpdate(ctx, sector)
	}
	call := workerCall{
		task: WorkerTask{
			TraceContext: propagation.Inject(ctx), //传播trace-id
			Type:         WorkerFinalizeReplicaUpdate,
			ProofType:    sector.ProofType,
			SectorID:     sector.ID,
			StoreUnseal:  sector.StoreUnseal,
			Snap:               true,
			SectorRepairStatus: sector.SectorRepairStatus,
		},
		ret: make(chan SealRes),
	}
	// send to remote worker
	select {
	case _finalizeReplicaUpdateTasks <- call:
		return sb.finalizeReplicaUpdateRemote(call)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sb *Sealer) replicaUpdateRemote(call workerCall) (storiface.ReplicaUpdateOut, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return ret.ReplicaUpdateOut, xerrors.New(ret.Err)
		}
		return ret.ReplicaUpdateOut, nil
	case <-sb.stopping:
		return storiface.ReplicaUpdateOut{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) ReplicaUpdate(ctx context.Context, sector storiface.SectorRef, pieces []abi.PieceInfo) (out storiface.ReplicaUpdateOut, err error) {
	log.Infof("DEBUG:ReplicaUpdate in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:ReplicaUpdate out,%+v", sector.ID)
	// uprade SectorRef
	atomic.AddInt32(&_replicaUpdateWait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_replicaUpdateWait, -1)
		return sb.replicaUpdate(ctx, sector, pieces)
	}
	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerReplicaUpdate,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			SectorRepairStatus: sector.SectorRepairStatus,
			Pieces: pieces,
		},
		ret: make(chan SealRes),
	}
	// send to remote worker
	select {
	case _replicaUpdateTasks <- call:
		return sb.replicaUpdateRemote(call)
	case <-ctx.Done():
		return storiface.ReplicaUpdateOut{}, ctx.Err()
	}
}

func (sb *Sealer) proveReplicaUpdate1Remote(call workerCall) (storiface.ReplicaVanillaProofs, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return ret.ReplicaVanillaProofs, xerrors.New(ret.Err)
		}
		return ret.ReplicaVanillaProofs, nil
	case <-sb.stopping:
		return storiface.ReplicaVanillaProofs{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) ProveReplicaUpdate1(ctx context.Context, sector storiface.SectorRef, sectorKey, newSealed, newUnsealed cid.Cid) (storiface.ReplicaVanillaProofs, error) {
	log.Infof("DEBUG:ProveReplicaUpdate1 in(remote:%t),%+v, d:%s r:%s", sb.remoteCfg.SealSector, sector.ID, newUnsealed, newSealed)
	defer log.Infof("DEBUG:ProveReplicaUpdate1 out,%+v", sector.ID)
	// uprade SectorRef
	atomic.AddInt32(&_proveReplicaUpdate1Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_proveReplicaUpdate1Wait, -1)
		return sb.proveReplicaUpdate1(ctx, sector, sectorKey, newSealed, newUnsealed)
	}
	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerProveReplicaUpdate1,
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
	case _proveReplicaUpdate1Tasks <- call:
		return sb.proveReplicaUpdate1Remote(call)
	case <-ctx.Done():
		return storiface.ReplicaVanillaProofs{}, ctx.Err()
	}
}

func (sb *Sealer) proveReplicaUpdate2Remote(call workerCall) (storiface.ReplicaUpdateProof, error) {
	select {
	case ret := <-call.ret:
		if ret.Err != "" {
			return ret.ReplicaUpdateProof, xerrors.New(ret.Err)
		}
		return ret.ReplicaUpdateProof, nil
	case <-sb.stopping:
		return storiface.ReplicaUpdateProof{}, xerrors.New("sectorbuilder stopped")
	}
}

func (sb *Sealer) ProveReplicaUpdate2(ctx context.Context, sector storiface.SectorRef, sectorKey, newSealed, newUnsealed cid.Cid, vanillaProofs storiface.ReplicaVanillaProofs) (storiface.ReplicaUpdateProof, error) {
	log.Infof("DEBUG:ProveReplicaUpdate2 in(remote:%t),%+v", sb.remoteCfg.SealSector, sector.ID)
	defer log.Infof("DEBUG:ProveReplicaUpdate2 out,%+v", sector.ID)
	// uprade SectorRef
	atomic.AddInt32(&_proveReplicaUpdate2Wait, 1)
	if !sb.remoteCfg.SealSector {
		atomic.AddInt32(&_proveReplicaUpdate2Wait, -1)
		return sb.proveReplicaUpdate2(ctx, sector, sectorKey, newSealed, newUnsealed, vanillaProofs)
	}
	call := workerCall{
		task: WorkerTask{
			TraceContext:       propagation.Inject(ctx), //传播trace-id
			Snap:               true,
			Type:               WorkerProveReplicaUpdate2,
			ProofType:          sector.ProofType,
			SectorID:           sector.ID,
			SectorRepairStatus: sector.SectorRepairStatus,
			SectorKey:   sectorKey,
			NewSealed:   newSealed,
			NewUnsealed: newUnsealed,
			VanillaProofs: vanillaProofs,
		},
		ret: make(chan SealRes),
	}
	// send to remote worker
	select {
	case _proveReplicaUpdate2Tasks <- call:
		return sb.proveReplicaUpdate2Remote(call)
	case <-ctx.Done():
		return storiface.ReplicaUpdateProof{}, ctx.Err()
	}
}
