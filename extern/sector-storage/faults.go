package sectorstorage

import (
	"context"
	"time"

	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
)

// FaultTracker TODO: Track things more actively
type FaultTracker interface {
	CheckProvable(ctx context.Context, sectors []storage.SectorRef, rg storiface.RGetter, timeout time.Duration) (all []ffiwrapper.ProvableStat, good []ffiwrapper.ProvableStat, bad []ffiwrapper.ProvableStat, err error)
}

// CheckProvable returns unprovable sectors
func (m *Manager) CheckProvable(ctx context.Context, sectors []storage.SectorRef, rg storiface.RGetter, timeout time.Duration) ([]ffiwrapper.ProvableStat, []ffiwrapper.ProvableStat, []ffiwrapper.ProvableStat, error) {
	return ffiwrapper.CheckProvable(ctx, sectors, rg, timeout)
}

var _ FaultTracker = &Manager{}
