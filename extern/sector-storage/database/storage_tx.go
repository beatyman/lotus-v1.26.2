package database

import (
	"sync"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

// simulate transaction.
type StorageTx struct {
	SectorId string
}

var (
	allocateMux  = sync.Mutex{}
	allocatePool = map[string]int64{}
)

func PrepareStorage(sectorId, fromIp string) (*StorageTx, *StorageInfo, error) {
	ssInfo, err := GetSectorStorage(sectorId)
	if err != nil {
		return nil, nil, errors.As(err, sectorId)
	}
	info := &ssInfo.StorageInfo
	db := GetDB()
	// has allocated
	if ssInfo.StorageInfo.ID > 0 {
		allocateMux.Lock()
		_, ok := allocatePool[sectorId]
		allocateMux.Unlock()
		if ok {
			return &StorageTx{sectorId}, &ssInfo.StorageInfo, nil
		}
		// prepare to transfer
	} else {
		// allocate new
		if err := database.QueryStruct(
			db, info,
			"SELECT * FROM storage_info WHERE mount_transf_uri like '%?%'",
			fromIp,
		); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return nil, nil, errors.As(err, sectorId)
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
	AND cur_work<max_work
	AND (used_size+sector_size*(cur_work+1)+keep_size)<=max_size
	ORDER BY cast(cur_work as real)/cast(max_work as real), max_size-used_size desc
	LIMIT 1
	`); err != nil {
				return nil, nil, errors.As(err, sectorId)
			}
		}
	}

	// allocate data
	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work+1 WHERE id=?", info.ID); err != nil {
		return nil, nil, errors.As(err)
	}
	if _, err := db.Exec("UPDATE sector_info SET storage_id=? WHERE id=?", info.ID, sectorId); err != nil {
		return nil, nil, errors.As(err, sectorId)
	}
	return &StorageTx{SectorId: sectorId}, info, nil
}
func (tx *StorageTx) Commit() error {
	ssInfo, err := GetSectorStorage(tx.SectorId)
	if err != nil {
		return errors.As(err, tx.SectorId)
	}
	// no prepare
	if ssInfo.StorageInfo.ID == 0 {
		return nil
	}

	storage := ssInfo.StorageInfo
	db := GetDB()
	if _, err := db.Exec("UPDATE sector_info set storage_id=? WHERE id=?", storage.ID, tx.SectorId); err != nil {
		return errors.As(err, *tx)
	}
	if _, err := db.Exec("UPDATE storage_info SET used_size=used_size+sector_size,cur_work=cur_work-1 WHERE id=?", storage.ID); err != nil {
		return errors.As(err, *tx)
	}
	return nil
}
func (tx *StorageTx) Rollback() error {
	return cancelStorage(tx.SectorId)
}

func cancelStorage(sectorId string) error {
	ssInfo, err := GetSectorStorage(sectorId)
	if err != nil {
		return errors.As(err, sectorId)
	}
	// no prepare
	if ssInfo.StorageInfo.ID == 0 {
		return nil
	}

	storage := ssInfo.StorageInfo
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET cur_work=cur_work-1 WHERE id=?", storage.ID); err != nil {
		return errors.As(err, sectorId, storage.ID)
	}
	allocateMux.Lock()
	delete(allocatePool, sectorId)
	allocateMux.Unlock()

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
