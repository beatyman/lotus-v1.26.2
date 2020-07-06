// +build debug 2k

package build

import (
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/abi/big"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/filecoin-project/specs-actors/actors/builtin/verifreg"
)

func init() {
	power.ConsensusMinerMinPower = big.NewInt(2048)
	miner.SupportedProofTypes = map[abi.RegisteredSealProof]struct{}{
		abi.RegisteredSealProof_StackedDrg2KiBV1:   {},
		abi.RegisteredSealProof_StackedDrg8MiBV1:   {},
		abi.RegisteredSealProof_StackedDrg512MiBV1: {},
		abi.RegisteredSealProof_StackedDrg32GiBV1:  {},
		abi.RegisteredSealProof_StackedDrg64GiBV1:  {},
	}
	verifreg.MinVerifiedDealSize = big.NewInt(256)

	BuildType |= Build2k
}

var SectorSizes = []abi.SectorSize{
	2048,
	512 << 20,
	32 << 30,
}

// Seconds
const BlockDelay = 10
const BlockDelaySecs = uint64(2)

const PropagationDelaySecs = uint64(3)

// SlashablePowerDelay is the number of epochs after ElectionPeriodStart, after
// which the miner is slashed
//
// Epochs
const SlashablePowerDelay = 20

// Epochs
const InteractivePoRepConfidence = 6
