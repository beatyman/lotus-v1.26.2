package sectorstorage

import (
	"context"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
)

// FaultTracker TODO: Track things more actively
type FaultTracker interface {
	CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []database.SectorFile, timeout time.Duration) (all []ffiwrapper.ProvableStat, good []database.SectorFile, bad []database.SectorFile, err error)
}

// CheckProvable returns unprovable sectors
func (m *Manager) CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []database.SectorFile, timeout time.Duration) ([]ffiwrapper.ProvableStat, []database.SectorFile, []database.SectorFile, error) {
	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, nil, nil, err
	}
	lstor := &readonlyProvider{stor: m.localStore}
	repo := lstor.RepoPath()
	return ffiwrapper.CheckProvable(ctx, repo, ssize, sectors, timeout)
}

var _ FaultTracker = &Manager{}
