package sectorstorage

import (
	"context"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
)

// FaultTracker TODO: Track things more actively
type FaultTracker interface {
	CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []storage.SectorFile, timeout time.Duration) (all []ffiwrapper.ProvableStat, good []ffiwrapper.ProvableStat, bad []ffiwrapper.ProvableStat, err error)
}

// CheckProvable returns unprovable sectors
func (m *Manager) CheckProvable(ctx context.Context, spt abi.RegisteredSealProof, sectors []storage.SectorFile, timeout time.Duration) ([]ffiwrapper.ProvableStat, []ffiwrapper.ProvableStat, []ffiwrapper.ProvableStat, error) {
	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, nil, nil, err
	}
	return ffiwrapper.CheckProvable(ctx, ssize, sectors, timeout)
}

var _ FaultTracker = &Manager{}
