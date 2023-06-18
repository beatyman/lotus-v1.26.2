package storage

import (
	"fmt"
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
	"golang.org/x/xerrors"
)

// copy from sector-storage/stores/filetype.go
func ParseSectorID(baseName string) (abi.SectorID, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
	if err != nil {
		read, err = fmt.Sscanf(baseName, "s-f0%d-%d", &mid, &n)
		if err != nil {
			return abi.SectorID{}, xerrors.Errorf("sscanf sector name ('%s'): %w", baseName, err)
		}
	}

	if read != 2 {
		return abi.SectorID{}, xerrors.Errorf("parseSectorID expected to scan 2 values, got %d", read)
	}

	return abi.SectorID{
		Miner:  mid,
		Number: n,
	}, nil
}

type SectorFile struct {
	SectorId string

	SealedRepo        string
	SealedStorageId   int64  // 0 not filled.
	SealedStorageType string // empty not filled.

	UnsealedRepo        string
	UnsealedStorageId   int64  // 0 not filled.
	UnsealedStorageType string // empty not filled.
	IsMarketSector      bool
}

func (f *SectorFile) HasRepo() bool {
	return len(f.SealedRepo) > 0 && len(f.UnsealedRepo) > 0
}

func (f *SectorFile) SectorID() abi.SectorID {
	id, err := ParseSectorID(f.SectorId)
	if err != nil {
		// should not reach here.
		panic(err)
	}
	return id
}

func (f *SectorFile) UnsealedFile() string {
	return filepath.Join(f.UnsealedRepo, "unsealed", f.SectorId)
}
func (f *SectorFile) SealedFile() string {
	return filepath.Join(f.SealedRepo, "sealed", f.SectorId)
}
func (f *SectorFile) CachePath() string {
	return filepath.Join(f.SealedRepo, "cache", f.SectorId)
}
func (f *SectorFile) UpdateFile() string {
	return filepath.Join(f.SealedRepo, "update", f.SectorId)
}
func (f *SectorFile) UpdateCachePath() string {
	return filepath.Join(f.SealedRepo, "update-cache", f.SectorId)
}
