package database

import (
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type SectorRebuild struct {
	ID            string `db:"id"`
	StorageSealed uint64 `db:"storage_sealed"` // 存储id

}

func GetSectorRebuildList() ([]SectorRebuild, error) {
	db := GetDB()
	infos := []SectorRebuild{}
	// TODO: index
	if err := database.QueryStructs(db, &infos, "SELECT id,storage_sealed FROM sector_rebuild"); err != nil {
		return nil, errors.As(err)
	}
	return infos, nil
}
