package sectorstorage

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
)

type readonlyProvider struct {
	index stores.SectorIndex
	stor  *stores.Local
	spt   abi.RegisteredSealProof
}

func (l *readonlyProvider) RepoPath() string {
	local, err := l.stor.Local(context.TODO())
	if err != nil {
		panic(err)
	}
	for _, p := range local {
		if p.CanStore {
			return p.LocalPath
		}
	}
	panic("No RepoPath")
}

func (l *readonlyProvider) AcquireSector(ctx context.Context, id abi.SectorID, existing stores.SectorFileType, allocate stores.SectorFileType, sealing stores.PathType) (stores.SectorPaths, func(), error) {
	return stores.HLMSectorPath(id, l.RepoPath()), func() {}, nil

	if allocate != stores.FTNone {
		return stores.SectorPaths{}, nil, xerrors.New("read-only storage")
	}

	ssize, err := l.spt.SectorSize()
	if err != nil {
		return stores.SectorPaths{}, nil, xerrors.Errorf("failed to determine sector size: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	// use TryLock to avoid blocking
	locked, err := l.index.StorageTryLock(ctx, id, existing, stores.FTNone)
	if err != nil {
		cancel()
		return stores.SectorPaths{}, nil, xerrors.Errorf("acquiring sector lock: %w", err)
	}
	if !locked {
		cancel()
		return stores.SectorPaths{}, nil, xerrors.Errorf("failed to acquire sector lock")
	}

	p, _, err := l.stor.AcquireSector(ctx, id, ssize, existing, allocate, sealing, stores.AcquireMove)

	return p, cancel, err
}
