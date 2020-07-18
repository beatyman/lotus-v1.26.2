// +build hlm

package build

func init() {
	InsecurePoStValidation = true
	BuildType |= BuildDebug
}

func init() {
	power.ConsensusMinerMinPower = big.NewInt(1024 << 30)
	miner.SupportedProofTypes = map[abi.RegisteredSealProof]struct{}{
		abi.RegisteredSealProof_StackedDrg2KiBV1:   {},
		abi.RegisteredSealProof_StackedDrg8MiBV1:   {},
		abi.RegisteredSealProof_StackedDrg512MiBV1: {},
		abi.RegisteredSealProof_StackedDrg32GiBV1:  {},
		abi.RegisteredSealProof_StackedDrg64GiBV1:  {},
	}
}

const BlockDelaySecs = uint64(builtin.EpochDurationSeconds)

const PropagationDelaySecs = uint64(6)
