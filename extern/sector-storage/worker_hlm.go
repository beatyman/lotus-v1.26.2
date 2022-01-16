package sectorstorage

import (
	"bufio"
	"context"
	"github.com/filecoin-project/dagstore/mount"
	"github.com/filecoin-project/lotus/extern/sector-storage/fr32"
	"io"
	"os"
	"runtime"

	"github.com/elastic/go-sysinfo"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"
	storage2 "github.com/filecoin-project/specs-storage/storage"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/sealtasks"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"

	"github.com/gwaylib/errors"
)

type hlmWorker struct {
	remoteCfg       ffiwrapper.RemoteCfg
	storage         stores.Store
	localStore      *stores.Local
	sindex          stores.SectorIndex
	noSwap          bool
	envLookup       EnvFunc
	ignoreResources bool
	sb              *ffiwrapper.Sealer
}

func NewHlmWorker(remoteCfg ffiwrapper.RemoteCfg, store stores.Store, local *stores.Local, sindex stores.SectorIndex) (*hlmWorker, error) {
	w := &hlmWorker{
		remoteCfg:       remoteCfg,
		storage:         store,
		localStore:      local,
		sindex:          sindex,
		envLookup:       os.LookupEnv,
		noSwap:          true,
		ignoreResources: true,
	}
	sb, err := ffiwrapper.New(remoteCfg, w)
	if err != nil {
		return nil, errors.As(err)
	}
	w.sb = sb
	return w, nil
}

func (l *hlmWorker) RepoPath() string {
	paths, err := l.localStore.Local(context.TODO())
	if err != nil {
		panic(err)
	}
	for _, p := range paths {
		if p.CanStore {
			return p.LocalPath
		}
	}
	panic("No RepoPath")
}

func (l *hlmWorker) AcquireSector(ctx context.Context, sector storage.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, sealing storiface.PathType) (storiface.SectorPaths, func(), error) {
	return stores.HLMSectorPath(sector.ID, l.RepoPath()), func() {}, nil
}

func (l *hlmWorker) NewSector(ctx context.Context, sector storage.SectorRef) error {
	return l.sb.NewSector(ctx, sector)
}

func (l *hlmWorker) PledgeSector(ctx context.Context, sectorID storage.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, sizes ...abi.UnpaddedPieceSize) ([]abi.PieceInfo, error) {
	return l.sb.PledgeSector(ctx, sectorID, existingPieceSizes, sizes...)
}

func (l *hlmWorker) AddPiece(ctx context.Context, sector storage.SectorRef, epcs []abi.UnpaddedPieceSize, sz abi.UnpaddedPieceSize, r io.Reader) (abi.PieceInfo, error) {
	return l.sb.AddPiece(ctx, sector, epcs, sz, r)
}

func (l *hlmWorker) SealPreCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, pieces []abi.PieceInfo) (out storage2.PreCommit1Out, err error) {
	return l.sb.SealPreCommit1(ctx, sector, ticket, pieces)
}

func (l *hlmWorker) SealPreCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage2.PreCommit1Out) (cids storage2.SectorCids, err error) {
	return l.sb.SealPreCommit2(ctx, sector, phase1Out)
}

func (l *hlmWorker) SealCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage2.SectorCids) (output storage2.Commit1Out, err error) {
	return l.sb.SealCommit1(ctx, sector, ticket, seed, pieces, cids)
}

func (l *hlmWorker) SealCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage2.Commit1Out) (proof storage2.Proof, err error) {
	return l.sb.SealCommit2(ctx, sector, phase1Out)
}

// union c1 and c2
func (l *hlmWorker) SealCommit(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage2.SectorCids) (storage.Proof, error) {
	return l.sb.SealCommit(ctx, sector, ticket, seed, pieces, cids)
}

func (l *hlmWorker) FinalizeSector(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage2.Range) error {
	if err := l.sb.FinalizeSector(ctx, sector, keepUnsealed); err != nil {
		return xerrors.Errorf("finalizing sector: %w", err)
	}
	// TODO: release unsealed
	//if len(keepUnsealed) == 0 {
	//	if err := l.storage.Remove(ctx, sector.ID, storiface.FTUnsealed, true); err != nil {
	//		return xerrors.Errorf("removing unsealed data: %w", err)
	//	}
	//	var err error
	//	sector, err = database.FillSectorFile(sector, l.sb.RepoPath())
	//	if err != nil {
	//		return errors.As(err)
	//	}
	//	if sector.HasRepo() {
	//		log.Warnf("Remove file:%s", sector.UnsealedFile())
	//		return os.Remove(sector.UnsealedFile())
	//	}
	//}

	return nil
}

func (l *hlmWorker) ReleaseUnsealed(ctx context.Context, sector abi.SectorID, safeToFree []storage2.Range) error {
	return xerrors.Errorf("implement me")
}

func (l *hlmWorker) Remove(ctx context.Context, sector storage.SectorRef) error {
	var err error

	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTSealed, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
	}
	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTCache, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (cache): %w", rerr))
	}
	if rerr := l.storage.Remove(ctx, sector.ID, storiface.FTUnsealed, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
	}
	var rerr error
	sector, rerr = database.FillSectorFile(sector, l.sb.RepoPath())
	if err != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (db): %w", rerr))
	} else if sector.HasRepo() {
		// SPEC:Automatic deletion of the sealed files is not safe and requires manual confirmation.
		//switch sector.SealedStorageType {
		//case database.MOUNT_TYPE_HLM:
		//	// Remove seaeld file.
		//	stor, rerr := database.GetStorage(sector.SealedStorageId)
		//	if rerr != nil {
		//		err = multierror.Append(err, xerrors.Errorf("removing sector (storage): %w", rerr))
		//		return errors.As(rerr)
		//	}
		//	sid := sector.SectorId
		//	auth := hlmclient.NewAuthClient(stor.MountAuthUri, stor.MountAuth)
		//	ctx := context.TODO()
		//	token, rerr := auth.NewFileToken(ctx, sid)
		//	if rerr != nil {
		//		err = multierror.Append(err, xerrors.Errorf("removing sector (auth): %w", rerr))
		//		return errors.As(rerr)
		//	}
		//	fc := hlmclient.NewHttpClient(stor.MountTransfUri, sid, string(token))
		//	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		//		err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
		//		return errors.As(rerr)
		//	}
		//	// TODO: scale the storage used_size
		//	// pass
		//
		//default:
		//	log.Warnf("Remove file:%s", sector.SealedFile())
		//	if rerr := os.Remove(sector.SealedFile()); err != nil {
		//		err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
		//	}
		//	log.Warnf("Remove file:%s", sector.CachePath())
		//	if rerr := os.RemoveAll(sector.CachePath()); err != nil {
		//		err = multierror.Append(err, xerrors.Errorf("removing sector (cache): %w", rerr))
		//	}
		//}

		switch sector.UnsealedStorageType {
		case database.MOUNT_TYPE_HLM:
			// Remove unseaeld file.
			stor, rerr := database.GetStorage(sector.UnsealedStorageId)
			if rerr != nil {
				err = multierror.Append(err, xerrors.Errorf("removing sector (storage): %w", rerr))
				return errors.As(rerr)
			}
			sid := sector.SectorId
			auth := hlmclient.NewAuthClient(stor.MountAuthUri, stor.MountAuth)
			ctx := context.TODO()
			token, rerr := auth.NewFileToken(ctx, sid)
			if rerr != nil {
				err = multierror.Append(err, xerrors.Errorf("removing sector (auth): %w", rerr))
				return errors.As(rerr)
			}
			fc := hlmclient.NewHttpClient(stor.MountTransfUri, sid, string(token))
			if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
				err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
				return errors.As(rerr)
			}
			// TODO: scale the storage used_size
		default:
			log.Warnf("Remove file:%s", sector.UnsealedFile())
			if rerr := os.Remove(sector.UnsealedFile()); err != nil {
				err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
			}
		}
	}

	return err
}

func (l *hlmWorker) UnsealPiece(ctx context.Context, sector storage.SectorRef, index storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, cid cid.Cid) error {
	return l.sb.UnsealPiece(ctx, sector, index, size, randomness, cid)
}
func (l *hlmWorker) ReadPieceStorageInfo(ctx context.Context, sector storage.SectorRef) (database.SectorStorage, error) {
	info, err := database.GetSectorStorage(storage.SectorName(sector.ID))
	if err != nil {
		return database.SectorStorage{}, err
	}
	return *info, nil
}
func (l *hlmWorker)readPiece(ctx context.Context, sector storage.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error)  {
	// try read the exist unsealed.
	ctx, cancel := context.WithCancel(ctx)
	rg, done, err := l.sb.PieceReader(ctx, sector, abi.PaddedPieceSize(pieceOffset.Padded()), size.Padded())
	if err != nil {
		cancel()
		return nil, false, errors.As(err)
	} else if done {
		cancel()
		buf := make([]byte, fr32.BufSize(size.Padded()))
		pr, err := (&pieceReader{
			ctx: ctx,
			getReader: func(ctx context.Context, startOffset uint64) (io.ReadCloser, error) {
				startOffsetAligned := storiface.UnpaddedByteIndex(startOffset / 127 * 127) // floor to multiple of 127

				r, err := rg(startOffsetAligned.Padded())
				if err != nil {
					return nil, xerrors.Errorf("getting reader at +%d: %w", startOffsetAligned, err)
				}

				upr, err := fr32.NewUnpadReaderBuf(r, size.Padded(), buf)
				if err != nil {
					r.Close() // nolint
					return nil, xerrors.Errorf("creating unpadded reader: %w", err)
				}

				bir := bufio.NewReaderSize(upr, 127)
				if startOffset > uint64(startOffsetAligned) {
					if _, err := bir.Discard(int(startOffset - uint64(startOffsetAligned))); err != nil {
						r.Close() // nolint
						return nil, xerrors.Errorf("discarding bytes for startOffset: %w", err)
					}
				}
				return struct {
					io.Reader
					io.Closer
				}{
					Reader: bir,
					Closer: funcCloser(func() error {
						return r.Close()
					}),
				}, nil
			},
			len:      size,
			onClose:  cancel,
			pieceCid: unsealed,
		}).init()
		if err != nil || pr == nil { // pr == nil to make sure we don't return typed nil
			cancel()
			return nil,true, err
		}
		cancel()
		return pr, true, nil
	}
	cancel()
	return nil,false,errors.New("readPiece not done")
}
func (l *hlmWorker)ReadPiece(ctx context.Context, sector storage.SectorRef, pieceOffset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, ticket abi.SealRandomness, unsealed cid.Cid) (mount.Reader, bool, error){
	// acquire a lock purely for reading unsealed sectors
	var err error
	sector, err = database.FillSectorFile(sector, l.sb.RepoPath())
	if err != nil {
		return nil, false, errors.As(err)
	}
	if err := database.AddMarketRetrieve(storage.SectorName(sector.ID)); err != nil {
		return nil, false, errors.As(err)
	}
	// unseal data will expire by 30 days if no visitor.
	l.sb.ExpireAllMarketRetrieve()
	r, done, err := l.readPiece(ctx, sector, pieceOffset, size,ticket,unsealed)
	if err != nil {
		return nil, false, errors.As(err)
	} else if done {
		return r, true, nil
	}
	// unsealed not found, do unseal and then read it.
	if err := l.sb.UnsealPiece(ctx, sector,pieceOffset, size,ticket, unsealed); err != nil {
		return nil, false, errors.As(err, sector, pieceOffset,size, unsealed)
	}
	return l.readPiece(ctx, sector, pieceOffset, size,ticket,unsealed)
}

func (l *hlmWorker) TaskTypes(context.Context) (map[sealtasks.TaskType]struct{}, error) {
	return nil, errors.New("no implements")
}

func (l *hlmWorker) Paths(ctx context.Context) ([]stores.StoragePath, error) {
	return l.localStore.Local(ctx)
}

func (l *hlmWorker) Info(context.Context) (storiface.WorkerInfo, error) {
	hostname, err := os.Hostname() // TODO: allow overriding from config
	if err != nil {
		panic(err)
	}

	gpus, err := ffi.GetGPUDevices()
	if err != nil {
		log.Errorf("getting gpu devices failed: %+v", err)
	}

	memPhysical, memUsed, memSwap, memSwapUsed, err := l.memInfo()
	if err != nil {
		return storiface.WorkerInfo{}, xerrors.Errorf("getting memory info: %w", err)
	}

	resEnv, err := storiface.ParseResourceEnv(func(key, def string) (string, bool) {
		return l.envLookup(key)
	})
	if err != nil {
		return storiface.WorkerInfo{}, xerrors.Errorf("interpreting resource env vars: %w", err)
	}

	return storiface.WorkerInfo{
		Hostname:        hostname,
		IgnoreResources: l.ignoreResources,
		Resources: storiface.WorkerResources{
			MemPhysical: memPhysical,
			MemUsed:     memUsed,
			MemSwap:     memSwap,
			MemSwapUsed: memSwapUsed,
			CPUs:        uint64(runtime.NumCPU()),
			GPUs:        gpus,
			Resources:   resEnv,
		},
	}, nil
}

func (l *hlmWorker) memInfo() (memPhysical, memUsed, memSwap, memSwapUsed uint64, err error) {
	h, err := sysinfo.Host()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	mem, err := h.Memory()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	memPhysical = mem.Total
	// mem.Available is memory available without swapping, it is more relevant for this calculation
	memUsed = mem.Total - mem.Available
	memSwap = mem.VirtualTotal
	memSwapUsed = mem.VirtualUsed

	if cgMemMax, cgMemUsed, cgSwapMax, cgSwapUsed, err := cgroupV1Mem(); err == nil {
		if cgMemMax > 0 && cgMemMax < memPhysical {
			memPhysical = cgMemMax
			memUsed = cgMemUsed
		}
		if cgSwapMax > 0 && cgSwapMax < memSwap {
			memSwap = cgSwapMax
			memSwapUsed = cgSwapUsed
		}
	}

	if cgMemMax, cgMemUsed, cgSwapMax, cgSwapUsed, err := cgroupV2Mem(); err == nil {
		if cgMemMax > 0 && cgMemMax < memPhysical {
			memPhysical = cgMemMax
			memUsed = cgMemUsed
		}
		if cgSwapMax > 0 && cgSwapMax < memSwap {
			memSwap = cgSwapMax
			memSwapUsed = cgSwapUsed
		}
	}

	if l.noSwap {
		memSwap = 0
		memSwapUsed = 0
	}

	return memPhysical, memUsed, memSwap, memSwapUsed, nil
}

func (l *hlmWorker) Closing(ctx context.Context) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

func (l *hlmWorker) Close() error {
	return nil
}
