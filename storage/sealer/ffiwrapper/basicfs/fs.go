package basicfs

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/database"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("ffiwrapper/basicfs")

type sectorFile struct {
	abi.SectorID
	storiface.SectorFileType
}

type Provider struct {
	Root string

	lk         sync.Mutex
	waitSector map[sectorFile]chan struct{}
}

func (b *Provider) RepoPath() string {
	if len(b.Root) == 0 {
		panic("Not Found Path")
	}
	return b.Root
}
func (b *Provider) AcquireSector(ctx context.Context, id storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, ptype storiface.PathType) (storiface.SectorPaths, func(), error) {
	if err := os.Mkdir(filepath.Join(b.Root, storiface.FTUnsealed.String()), 0755); err != nil && !os.IsExist(err) { // nolint
		return storiface.SectorPaths{}, nil, err
	}
	if err := os.Mkdir(filepath.Join(b.Root, storiface.FTSealed.String()), 0755); err != nil && !os.IsExist(err) { // nolint
		return storiface.SectorPaths{}, nil, err
	}
	if err := os.Mkdir(filepath.Join(b.Root, storiface.FTCache.String()), 0755); err != nil && !os.IsExist(err) { // nolint
		return storiface.SectorPaths{}, nil, err
	}
	if err := os.Mkdir(filepath.Join(b.Root, storiface.FTUpdate.String()), 0755); err != nil && !os.IsExist(err) { // nolint
		return storiface.SectorPaths{}, nil, err
	}
	if err := os.Mkdir(filepath.Join(b.Root, storiface.FTUpdateCache.String()), 0755); err != nil && !os.IsExist(err) { // nolint
		return storiface.SectorPaths{}, nil, err
	}
	//return stores.HLMSectorPath(id, b.RepoPath()), func() {}, nil

	done := func() {}

	out := storiface.SectorPaths{
		ID: id.ID,
	}

	for _, fileType := range storiface.PathTypes {
		if !existing.Has(fileType) && !allocate.Has(fileType) {
			continue
		}

		b.lk.Lock()
		if b.waitSector == nil {
			b.waitSector = map[sectorFile]chan struct{}{}
		}
		ch, found := b.waitSector[sectorFile{id.ID, fileType}]
		if !found {
			ch = make(chan struct{}, 1)
			b.waitSector[sectorFile{id.ID, fileType}] = ch
		}
		b.lk.Unlock()

		select {
		case ch <- struct{}{}:
		case <-ctx.Done():
			done()
			return storiface.SectorPaths{}, nil, ctx.Err()
		}

		path := filepath.Join(b.Root, fileType.String(), storiface.SectorName(id.ID))
		if fileType.String() == "unsealed" {
			//判unsealed目录是否有unsealed文件，如果有，则指向他，如果没有，则指向cc文件
			isExist, err := PathExists(path)
			if err == nil {
				//不存在则使用cc文件
				if !isExist {
					path = filepath.Join(b.Root, "sector_cc")
				}
			}
			if err != nil {
				log.Info("=========FS PathExists err ====", err)
			}
			log.Info("=========FS.go unseal path sector id ====,isExist:", id.ID, path, isExist)
		}

		prevDone := done
		done = func() {
			prevDone()
			<-ch
		}

		if !allocate.Has(fileType) {
			switch id.UnsealedStorageType {
			case database.MOUNT_TYPE_OSS:
			default:
				if _, err := os.Stat(path); os.IsNotExist(err) {
					done()
					return storiface.SectorPaths{}, nil, storiface.ErrSectorNotFound
				}
			}
		}

		storiface.SetPathByType(&out, fileType, path)
	}

	return out, done, nil
}

func (b *Provider) AcquireSectorCopy(ctx context.Context, id storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, ptype storiface.PathType) (storiface.SectorPaths, func(), error) {
	return b.AcquireSector(ctx, id, existing, allocate, ptype)
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
