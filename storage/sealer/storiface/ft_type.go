package storiface

import (
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"golang.org/x/xerrors"
)

var SectorHead = "s-f"

func MinerID(miner abi.ActorID) string {
	return fmt.Sprintf("%s0%d", SectorHead, miner)
}
func sectorName(sid abi.SectorID) string {
	return fmt.Sprintf("%s0%d-%d", SectorHead, sid.Miner, sid.Number)
}

func ParseMinerID(baseName string) (abi.ActorID, error) {
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-f0%d", &mid)
	if err != nil {
		read, err = fmt.Sscanf(baseName, "s-t0%d", &mid)
		if err != nil {
			return mid, xerrors.Errorf("sscanf sector name ('%s'): %w", baseName, err)
		}
	}

	if read != 1 {
		return mid, xerrors.Errorf("parseMinerID expected to scan 2 values, got %d", read)
	}
	return mid, nil
}
func parseSectorID(baseName string) (abi.SectorID, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-f0%d-%d", &mid, &n)
	if err != nil {
		read, err = fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
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
