package sectorstorage

import (
	"context"
	"time"

	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
)

// FaultTracker TODO: Track things more actively
type FaultTracker interface {
	CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []abi.SectorID, timeout time.Duration) (all []ffiwrapper.ProvableStat, bad []abi.SectorID, err error)
}

// CheckProvable returns unprovable sectors
func (m *Manager) CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []abi.SectorID, timeout time.Duration) ([]ffiwrapper.ProvableStat, []abi.SectorID, error) {
	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, nil, err
	}
	lstor := &readonlyProvider{stor: m.localStore}
	repo := lstor.RepoPath()
	return ffiwrapper.CheckProvable(repo, ssize, sectors, timeout)
}

var _ FaultTracker = &Manager{}
