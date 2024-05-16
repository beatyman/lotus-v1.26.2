package database

import (
	"fmt"
	"sync"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

var (
	ErrNoStorage = errors.New("No storage node for allocation")
)

// simulate transaction.
type StorageTx struct {
	SectorId string
	Kind     int

	// TODO: open this value
	storageId int64
}

func (s *StorageTx) Key() string {
	return fmt.Sprintf("%s-%d", s.SectorId, s.Kind)
}

var (
	allocateMux  = sync.Mutex{}
	allocatePool = map[string]int64{}
)

func PrepareStorage(sectorId, fromIp string, kind int) (*StorageTx, *StorageInfo, error) {
	_, storage, _ := RebuildSectorProcess(sectorId)
	if storage > 0 {
		tx := &StorageTx{SectorId: sectorId, Kind: STORAGE_KIND_SEALED}
		// force for rebuild storage
		tx.storageId = int64(storage)

		storageInfo, err := GetStorageInfo(tx.storageId)
		if err != nil {
			return nil, nil, errors.As(err, sectorId)
		}
		return tx, storageInfo, nil
	}

	ssInfo, err := GetSectorStorage(sectorId)
	if err != nil {
		return nil, nil, errors.As(err, sectorId)
	}

	tx := &StorageTx{SectorId: sectorId, Kind: kind}
	var info *StorageInfo
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		info = &ssInfo.SealedStorage
	case STORAGE_KIND_UNSEALED:
		info = &ssInfo.UnsealedStorage
	default:
		return nil, nil, errors.New("unknow kind").As(sectorId, fromIp, kind)
	}
	db := GetDB()
	// has allocated
	if info.ID > 0 {
		allocateMux.Lock()
		_, ok := allocatePool[tx.Key()]
		allocateMux.Unlock()
		if ok {
			return tx, info, nil
		}
		// prepare to transfer
	} else {
		// allocate new
		if err := database.QueryStruct(
			db, info,
			"SELECT * FROM storage_info WHERE kind=? AND mount_transf_uri like '%?%'",
			kind, fromIp,
		); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return nil, nil, errors.As(err, *tx)
			}
			// data not found
		} else {
			if info.UsedSize+info.SectorSize*int64(info.CurWork+1)+info.KeepSize > info.MaxSize {
				// no space for allocatoin, call next alloction.
				info = &StorageInfo{}
			}
		}

		// if allocate failed, make a default allocation.
		if info.ID == 0 {
			if err := database.QueryStruct(
				db, info,
				`
SELECT
	* 
FROM
	storage_info 
WHERE
	disable=0
	AND kind=?
	AND cur_work<max_work
	AND (used_size+sector_size*(cur_work+1)+keep_size)<=max_size
	ORDER BY cast(cur_work as real)/cast(max_work as real), max_size-used_size desc
	LIMIT 1
	`, tx.Kind); err != nil {
				if errors.ErrNoData.Equal(err) {
					return nil, nil, ErrNoStorage.As(*tx)
				}
				return nil, nil, errors.As(err, *tx)
			}
		}
	}

	// Allocate data
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		if _, err := db.Exec("UPDATE sector_info SET storage_sealed=? WHERE id=?", info.ID, sectorId); err != nil {
			return nil, nil, errors.As(err, sectorId)
		}
	case STORAGE_KIND_UNSEALED:
		if _, err := db.Exec("UPDATE sector_info SET storage_unsealed=? WHERE id=?", info.ID, sectorId); err != nil {
			return nil, nil, errors.As(err, sectorId)
		}
	}
	// Declaration of use the storage space
	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work+1 WHERE id=?", info.ID); err != nil {
		return nil, nil, errors.As(err)
	}
	tx.storageId = info.ID
	allocateMux.Lock()
	allocatePool[tx.Key()] = info.ID
	allocateMux.Unlock()

	// delete sector cache
	sectorFileCacheLk.Lock()
	delete(sectorFileCaches, sectorId)
	sectorFileCacheLk.Unlock()

	return tx, info, nil
}
func (tx *StorageTx) Commit() error {
	ssInfo, err := GetSectorStorage(tx.SectorId)
	if err != nil {
		return errors.As(err, *tx)
	}
	var info *StorageInfo
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		info = &ssInfo.SealedStorage
	case STORAGE_KIND_UNSEALED:
		info = &ssInfo.UnsealedStorage
	default:
		return errors.New("unknow kind").As(*tx)
	}

	// no prepare
	if info.ID == 0 {
		if tx.storageId == 0 {
			allocateMux.Lock()
			delete(allocatePool, tx.Key())
			allocateMux.Unlock()
			return nil
		}
		info.ID = tx.storageId
	}

	db := GetDB()

	// TODO: read from real storage
	if _, err := db.Exec("UPDATE storage_info SET used_size=used_size+sector_size,cur_work=cur_work-1 WHERE id=?", info.ID); err != nil {
		return errors.As(err, *tx)
	}

	allocateMux.Lock()
	delete(allocatePool, tx.Key())
	allocateMux.Unlock()
	return nil
}
func (tx *StorageTx) Rollback() error {
	allocateMux.Lock()
	delete(allocatePool, tx.Key())
	allocateMux.Unlock()

	ssInfo, err := GetSectorStorage(tx.SectorId)
	if err != nil {
		return errors.As(err, *tx)
	}
	var info *StorageInfo
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		info = &ssInfo.SealedStorage
	case STORAGE_KIND_UNSEALED:
		info = &ssInfo.UnsealedStorage
	default:
		return errors.New("unknow kind").As(*tx)
	}
	// no prepare
	if info.ID == 0 {
		if tx.storageId == 0 {
			return nil
		}
		info.ID = tx.storageId
	}
	db := GetDB()

	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work-1 WHERE id=?", info.ID); err != nil {
		return errors.As(err, *tx, info.ID)
	}

	return nil
}

// SPEC: only do this at system starting.
func ClearStorageWork() error {
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET cur_work=0"); err != nil {
		return errors.As(err)
	}
	allocateMux.Lock()
	allocatePool = map[string]int64{}
	allocateMux.Unlock()
	return nil
}

func AcquireStorageConnCount(sectorId string, kind int) error {
	ssInfo, err := GetSectorStorage(sectorId)
	if err != nil {
		return err
	}

	tx := &StorageTx{SectorId: sectorId, Kind: kind}
	var info *StorageInfo
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		info = &ssInfo.SealedStorage
	case STORAGE_KIND_UNSEALED:
		info = &ssInfo.UnsealedStorage
	default:
		return errors.New("unknow kind")
	}
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work+1 WHERE id=? and cur_work<max_work", info.ID); err != nil {
		return errors.As(err, *tx, info.ID)
	}
	return nil
}

func ReleaseStorageConnCount(sectorId string, kind int) error {
	ssInfo, err := GetSectorStorage(sectorId)
	if err != nil {
		return err
	}

	tx := &StorageTx{SectorId: sectorId, Kind: kind}
	var info *StorageInfo
	switch tx.Kind {
	case STORAGE_KIND_SEALED:
		info = &ssInfo.SealedStorage
	case STORAGE_KIND_UNSEALED:
		info = &ssInfo.UnsealedStorage
	default:
		return errors.New("unknow kind")
	}
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work-1 WHERE id=? and cur_work<max_work and cur_work>0", info.ID); err != nil {
		return errors.As(err, *tx, info.ID)
	}
	return nil
}
