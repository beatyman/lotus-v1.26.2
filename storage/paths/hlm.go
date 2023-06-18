package paths

import (
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func HLMSectorPath(sectorID abi.SectorID, repo string) storiface.SectorPaths {
	sectorName:=storiface.SectorName(sectorID)
	return storiface.SectorPaths{
		ID:       sectorID,
		Unsealed: filepath.Join(repo, "unsealed", sectorName),
		Sealed:   filepath.Join(repo, "sealed", sectorName),
		Cache:    filepath.Join(repo, "cache", sectorName),
	}
}
