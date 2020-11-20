package sectorstorage

import (
	"context"
	"io"
	"os"
	"runtime"

	"github.com/elastic/go-sysinfo"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"
	storage2 "github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/sealtasks"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"

	"github.com/gwaylib/errors"
)

type hlmWorker struct {
	remoteCfg  ffiwrapper.RemoteCfg
	storage    stores.Store
	localStore *stores.Local
	sindex     stores.SectorIndex

	sb ffiwrapper.Storage
}

func NewHlmWorker(remoteCfg ffiwrapper.RemoteCfg, store stores.Store, local *stores.Local, sindex stores.SectorIndex) (*hlmWorker, error) {
	w := &hlmWorker{
		remoteCfg:  remoteCfg,
		storage:    store,
		localStore: local,
		sindex:     sindex,
	}
	sb, err := ffiwrapper.New(remoteCfg, w)
	if err != nil {
		return nil, errors.As(err)
	}
	w.sb = sb
	return w, nil
}

func (l *hlmWorker) RepoPath() string {
	paths, err := l.localStore.Local(context.TODO())
	if err != nil {
		panic(err)
	}
	for _, p := range paths {
		if p.CanStore {
			return p.LocalPath
		}
	}
	panic("No RepoPath")
}

func (l *hlmWorker) AcquireSector(ctx context.Context, sector storage.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, sealing storiface.PathType) (storiface.SectorPaths, func(), error) {
	return stores.HLMSectorPath(sector.ID, l.RepoPath()), func() {}, nil
}

func (l *hlmWorker) NewSector(ctx context.Context, sector storage.SectorRef) error {
	return l.sb.NewSector(ctx, sector)
}

func (l *hlmWorker) AddPiece(ctx context.Context, sector storage.SectorRef, epcs []abi.UnpaddedPieceSize, sz abi.UnpaddedPieceSize, r io.Reader) (abi.PieceInfo, error) {
	return l.sb.AddPiece(ctx, sector, epcs, sz, r)
}

func (l *hlmWorker) SealPreCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, pieces []abi.PieceInfo) (out storage2.PreCommit1Out, err error) {
	return l.sb.SealPreCommit1(ctx, sector, ticket, pieces)
}

func (l *hlmWorker) SealPreCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage2.PreCommit1Out) (cids storage2.SectorCids, err error) {
	return l.sb.SealPreCommit2(ctx, sector, phase1Out)
}

func (l *hlmWorker) SealCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage2.SectorCids) (output storage2.Commit1Out, err error) {
	return l.sb.SealCommit1(ctx, sector, ticket, seed, pieces, cids)
}

func (l *hlmWorker) SealCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage2.Commit1Out) (proof storage2.Proof, err error) {
	return l.sb.SealCommit2(ctx, sector, phase1Out)
}

func (l *hlmWorker) FinalizeSector(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage2.Range) error {
	if err := l.sb.FinalizeSector(ctx, sector, keepUnsealed); err != nil {
		return xerrors.Errorf("finalizing sector: %w", err)
	}
	if len(keepUnsealed) == 0 {
		if err := l.storage.Remove(ctx, sector.ID, storiface.FTUnsealed, true); err != nil {
			return xerrors.Errorf("removing unsealed data: %w", err)
		}
	}

	return nil
}

func (l *hlmWorker) ReleaseUnsealed(ctx context.Context, sector abi.SectorID, safeToFree []storage2.Range) error {
	return xerrors.Errorf("implement me")
}

func (l *hlmWorker) Remove(ctx context.Context, sector storage.SectorRef) error {
	var err error

	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTSealed, true); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
	}
	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTCache, true); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (cache): %w", rerr))
	}
	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTUnsealed, true); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
	}

	return err
}

func (l *hlmWorker) UnsealPiece(ctx context.Context, sector storage.SectorRef, index storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, cid cid.Cid) error {
	return l.sb.UnsealPiece(ctx, sector, index, size, randomness, cid)
}

func (l *hlmWorker) ReadPiece(ctx context.Context, writer io.Writer, sector storage.SectorRef, index storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (bool, error) {
	// try read exist unsealed
	if err := l.sindex.StorageLock(ctx, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		return false, xerrors.Errorf("acquiring read sector lock: %w", err)
	}
	// passing 0 spt because we only need it when allowFetch is true
	best, err := l.sindex.StorageFindSector(ctx, sector.ID, storiface.FTUnsealed, 0, false)
	if err != nil {
		return false, xerrors.Errorf("read piece: checking for already existing unsealed sector: %w", err)
	}

	foundUnsealed := len(best) > 0
	if foundUnsealed { // append to existing
		// There is unsealed sector, see if we can read from it
		return l.sb.ReadPiece(ctx, writer, sector, index, size)
	}

	// unsealed not found, unseal and then read it.
	if err := l.sb.UnsealPiece(ctx, sector, index, size, ticket, unsealed); err != nil {
		return false, errors.As(err, sector, index, size, unsealed)
	}
	return l.sb.ReadPiece(ctx, writer, sector, index, size)
}

func (l *hlmWorker) TaskTypes(context.Context) (map[sealtasks.TaskType]struct{}, error) {
	return nil, errors.New("no implements")
}

func (l *hlmWorker) Paths(ctx context.Context) ([]stores.StoragePath, error) {
	return l.localStore.Local(ctx)
}

func (l *hlmWorker) Info(context.Context) (storiface.WorkerInfo, error) {
	hostname, err := os.Hostname() // TODO: allow overriding from config
	if err != nil {
		panic(err)
	}

	gpus, err := ffi.GetGPUDevices()
	if err != nil {
		log.Errorf("getting gpu devices failed: %+v", err)
	}

	h, err := sysinfo.Host()
	if err != nil {
		return storiface.WorkerInfo{}, xerrors.Errorf("getting host info: %w", err)
	}

	mem, err := h.Memory()
	if err != nil {
		return storiface.WorkerInfo{}, xerrors.Errorf("getting memory info: %w", err)
	}

	memSwap := mem.VirtualTotal

	return storiface.WorkerInfo{
		Hostname: hostname,
		Resources: storiface.WorkerResources{
			MemPhysical: mem.Total,
			MemSwap:     memSwap,
			MemReserved: mem.VirtualUsed + mem.Total - mem.Available, // TODO: sub this process
			CPUs:        uint64(runtime.NumCPU()),
			GPUs:        gpus,
		},
	}, nil
}

func (l *hlmWorker) Closing(ctx context.Context) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

func (l *hlmWorker) Close() error {
	return nil
}
