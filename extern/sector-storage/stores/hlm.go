package stores

import (
	"fmt"
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
)

func HLMSectorPath(sectorID abi.SectorID, repo string) storiface.SectorPaths {
	sectorName := fmt.Sprintf("s-t0%d-%d", sectorID.Miner, sectorID.Number)
	return storiface.SectorPaths{
		ID:       sectorID,
		Unsealed: filepath.Join(repo, "unsealed", sectorName),
		Sealed:   filepath.Join(repo, "sealed", sectorName),
		Cache:    filepath.Join(repo, "cache", sectorName),
	}
}
