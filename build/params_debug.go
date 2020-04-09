// +build debug

package build

import (
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/abi/big"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
)

func init() {
	InsecurePoStValidation = true

	power.ConsensusMinerMinPower = big.NewInt(2048)
}

var SectorSizes = []abi.SectorSize{
	2048,
	512 << 20,
	32 << 30,
}

// Seconds
const BlockDelay = 10

const PropagationDelay = 3

// SlashablePowerDelay is the number of epochs after ElectionPeriodStart, after
// which the miner is slashed
//
// Epochs
const SlashablePowerDelay = 20

// Epochs
const InteractivePoRepConfidence = 6
