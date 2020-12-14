package ffiwrapper

import (
	"context"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
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
}

func (sb *Sealer) Stop() {
	close(sb.stopping)
}

func (sb *Sealer) RepoPath() string {
	return sb.sectors.RepoPath()
}

func (sb *Sealer) pledgeSector(ctx context.Context, sectorID storage.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, sizes ...abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	if len(sizes) == 0 {
		log.Info("No sizes for pledge")
		return nil, nil
	}

	log.Infof("Pledge %+v, contains %+v", storage.SectorName(sectorID.ID), existingPieceSizes)

	out := make([]abi.PieceInfo, len(sizes))
	for i, size := range sizes {
		ppi, err := sb.AddPiece(ctx, sectorID, existingPieceSizes, size, NewNullReader(size))
		if err != nil {
			return nil, xerrors.Errorf("add piece: %w", err)
		}

		existingPieceSizes = append(existingPieceSizes, size)

		out[i] = ppi
	}

	return out, nil
}
