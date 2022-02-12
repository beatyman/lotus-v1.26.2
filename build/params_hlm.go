//go:build hlm
// +build hlm

package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	builtin2 "github.com/filecoin-project/specs-actors/v2/actors/builtin"
	"github.com/ipfs/go-cid"
)

var DrandSchedule = map[abi.ChainEpoch]DrandEnum{
	0: DrandMainnet,
}

const BootstrappersFile = "devnet.pi"
const GenesisFile = "devnet.car"
const GenesisNetworkVersion = network.Version14

var UpgradeBreezeHeight = abi.ChainEpoch(-1)

const BreezeGasTampingDuration = 0

var UpgradeSmokeHeight = abi.ChainEpoch(-1)
var UpgradeIgnitionHeight = abi.ChainEpoch(-2)
var UpgradeRefuelHeight = abi.ChainEpoch(-3)
var UpgradeTapeHeight = abi.ChainEpoch(-4)

var UpgradeAssemblyHeight = abi.ChainEpoch(-5)
var UpgradeLiftoffHeight = abi.ChainEpoch(-6)

var UpgradeKumquatHeight = abi.ChainEpoch(-7)
var UpgradeCalicoHeight = abi.ChainEpoch(-8)
var UpgradePersianHeight = abi.ChainEpoch(-9)
var UpgradeOrangeHeight = abi.ChainEpoch(-10)
var UpgradeClausHeight = abi.ChainEpoch(-11)

var UpgradeTrustHeight = abi.ChainEpoch(-12)

var UpgradeNorwegianHeight = abi.ChainEpoch(-13)

var UpgradeTurboHeight = abi.ChainEpoch(-14)

var UpgradeHyperdriveHeight = abi.ChainEpoch(-15)

var UpgradeChocolateHeight = abi.ChainEpoch(-16)

// 2022-02-10T19:23:00Z
const UpgradeOhSnapHeight = 1000000000000

func init() {
	policy.SetSupportedProofTypes(
		abi.RegisteredSealProof_StackedDrg2KiBV1,
		abi.RegisteredSealProof_StackedDrg512MiBV1,
		abi.RegisteredSealProof_StackedDrg32GiBV1,
		abi.RegisteredSealProof_StackedDrg64GiBV1,
	)

	policy.SetConsensusMinerMinPower(abi.NewStoragePower(10 << 30))
	SetAddressNetwork(address.Testnet)

	Devnet = true

	BuildType |= BuildHLM
}

const BlockDelaySecs = uint64(builtin2.EpochDurationSeconds)

const PropagationDelaySecs = uint64(6)

// BootstrapPeerThreshold is the minimum number peers we need to track for a sync worker to start
const BootstrapPeerThreshold = 1

var WhitelistedBlock = cid.Undef
