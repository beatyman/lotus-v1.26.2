// +build hlm

package build

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	builtin2 "github.com/filecoin-project/specs-actors/v2/actors/builtin"
)

var DrandSchedule = map[abi.ChainEpoch]DrandEnum{
	0: DrandMainnet,
}

const BootstrappersFile = "devnet.pi"
const GenesisFile = "devnet.car"

const UpgradeBreezeHeight = -1
const BreezeGasTampingDuration = 0

const UpgradeSmokeHeight = -1

const UpgradeIgnitionHeight = -2
const UpgradeRefuelHeight = -3

const UpgradeActorsV2Height = 10

const UpgradeTapeHeight = -4

const UpgradeLiftoffHeight = -5

const UpgradeKumquatHeight = 15

const UpgradeCalicoHeight = 20
const UpgradePersianHeight = 25

const UpgradeOrangeHeight = 27

const UpgradeClausHeight = 30

const UpgradeActorsV3Height = 69135 // ~2021-02-23 16:56:00.000+0800

func init() {
	policy.SetSupportedProofTypes(
		abi.RegisteredSealProof_StackedDrg2KiBV1,
		abi.RegisteredSealProof_StackedDrg512MiBV1,
		abi.RegisteredSealProof_StackedDrg32GiBV1,
		abi.RegisteredSealProof_StackedDrg64GiBV1,
	)
	policy.SetConsensusMinerMinPower(abi.NewStoragePower(10 << 30))
	BuildType |= BuildHLM
}

const BlockDelaySecs = uint64(builtin2.EpochDurationSeconds)

const PropagationDelaySecs = uint64(6)

// BootstrapPeerThreshold is the minimum number peers we need to track for a sync worker to start
const BootstrapPeerThreshold = 1
