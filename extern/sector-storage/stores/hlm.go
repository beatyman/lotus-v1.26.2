package stores

import (
	"fmt"
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
)

func HLMSectorPath(sectorID abi.SectorID, repo string) SectorPaths {
	sectorName := fmt.Sprintf("s-t0%d-%d", sectorID.Miner, sectorID.Number)
	return SectorPaths{
		ID:       sectorID,
		Unsealed: filepath.Join(repo, "unsealed", sectorName),
		Sealed:   filepath.Join(repo, "sealed", sectorName),
		Cache:    filepath.Join(repo, "cache", sectorName),
	}
}
