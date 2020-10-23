package ffiwrapper

import (
	"github.com/filecoin-project/go-state-types/abi"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("ffiwrapper")

type Sealer struct {
	sealProofType abi.RegisteredSealProof
	ssize         abi.SectorSize // a function of sealProofType and postProofType

	sectors  SectorProvider
	stopping chan struct{}

	//// for remote worker start
	remoteCfg RemoteCfg // if in remote mode, remote worker will be called.
	pauseSeal int32     // pause seal for base fee, zero is not pause, not zero is true.
	//// for remote worker end

}

func (sb *Sealer) Stop() {
	close(sb.stopping)
}

func (sb *Sealer) SectorSize() abi.SectorSize {
	return sb.ssize
}

func (sb *Sealer) SealProofType() abi.RegisteredSealProof {
	return sb.sealProofType
}

func (sb *Sealer) RepoPath() string {
	return sb.sectors.RepoPath()
}
