package ffiwrapper

import (
	"context"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"golang.org/x/xerrors"
	"sync"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("ffiwrapper")

type Sealer struct {
	sectors  SectorProvider
	stopping chan struct{}

	//// for remote worker start
	remoteCfg RemoteCfg // if in remote mode, remote worker will be called.
	pauseSeal int32     // pause seal for base fee, zero is not pause, not zero is true.
	//// for remote worker end

	postLk sync.Mutex

	unsealing sync.Map

	offlineWorker sync.Map
}

func (sb *Sealer) Stop() {
	close(sb.stopping)
}

func (sb *Sealer) RepoPath() string {
	return sb.sectors.RepoPath()
}
// Refer to : https://github.com/filecoin-project/lotus/blob/46e5cc9317a34325b0a9cae45aac582a03b9a788/extern/storage-sealing/garbage.go#L12
func (sb *Sealer) pledgeSector(ctx context.Context, sectorID storiface.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, sizes ...abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	if len(sizes) == 0 {
		log.Info("No sizes for pledge")
		return nil, nil
	}

	log.Infof("Pledge %+v, contains %+v, sizes %+v", storiface.SectorName(sectorID.ID), existingPieceSizes, sizes)

	out := make([]abi.PieceInfo, len(sizes))
	for i, size := range sizes {
		ppi, err := sb.AddPiece(ctx, sectorID, existingPieceSizes, size,  shared.NewNullPieceData(size))
		if err != nil {
			return nil, xerrors.Errorf("add piece: %w", err)
		}

		existingPieceSizes = append(existingPieceSizes, size)

		out[i] = ppi
	}

	return out, nil
}