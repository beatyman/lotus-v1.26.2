package sealer

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/storage/paths"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type readonlyProvider struct {
	index paths.SectorIndex
	stor  *paths.Local
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
func (l *readonlyProvider) AcquireSector(ctx context.Context, id storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, sealing storiface.PathType) (storiface.SectorPaths, func(), error) {
	return paths.HLMSectorPath(id.ID, l.RepoPath()), func() {}, nil
	if allocate != storiface.FTNone {
		return storiface.SectorPaths{}, nil, xerrors.New("read-only storage")
	}

	ctx, cancel := context.WithCancel(ctx)

	// use TryLock to avoid blocking
	locked, err := l.index.StorageTryLock(ctx, id.ID, existing, storiface.FTNone)
	if err != nil {
		cancel()
		return storiface.SectorPaths{}, nil, xerrors.Errorf("acquiring sector lock: %w", err)
	}
	if !locked {
		cancel()
		return storiface.SectorPaths{}, nil, xerrors.Errorf("failed to acquire sector lock")
	}

	p, _, err := l.stor.AcquireSector(ctx, id, existing, allocate, sealing, storiface.AcquireMove)

	return p, cancel, err
}

func (l *readonlyProvider) AcquireSectorCopy(ctx context.Context, id storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, ptype storiface.PathType) (storiface.SectorPaths, func(), error) {
	return storiface.SectorPaths{}, nil, xerrors.New("read-only storage")
}
