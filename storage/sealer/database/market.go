package database

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

var (
	exireMarketRetrieveLock = sync.Mutex{}
)

type MarketRetrieve struct {
	SectorId      string    `db:"sid"` // s-t01001-1
	RetrieveTimes int       `db:"retrieve_times"`
	RetrieveTime  time.Time `db:"retrieve_time"`
}

func GetMarketRetrieve(sid string) (*MarketRetrieve, error) {
	mdb := GetDB()
	info := &MarketRetrieve{}
	if err := database.QueryStruct(mdb, info, "SELECT * FROM market_retrieve WHERE sid=?", sid); err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}

func AddMarketRetrieve(sid string) error {
	db := GetDB()
	tx, err := db.Begin()
	if err != nil {
		return errors.As(err, sid)
	}
	exist := 0
	if err := database.QueryElem(tx, &exist, "SELECT count(*) FROM market_retrieve WHERE sid=?", sid); err != nil {
		database.Rollback(tx)
		return errors.As(err, sid)
	}
	if exist == 0 {
		info := &MarketRetrieve{SectorId: sid, RetrieveTimes: 1, RetrieveTime: time.Now()}
		if _, err := database.InsertStruct(tx, info, "market_retrieve"); err != nil {
			database.Rollback(tx)
			return errors.As(err, sid)
		}
	} else {
		now := time.Now()
		if _, err := tx.Exec("UPDATE market_retrieve SET updated_at=?, active=1, retrieve_times=retrieve_times+1, retrieve_time=? WHERE sid=?", now, now, sid); err != nil {
			database.Rollback(tx)
			return errors.As(err, sid)
		}
	}
	if err := tx.Commit(); err != nil {
		database.Rollback(tx)
		return errors.As(err, sid)
	}

	return nil
}

func GetMarketRetrieves(invalidTime time.Time) ([]MarketRetrieve, error) {
	db := GetDB()
	result := []MarketRetrieve{}
	if err := database.QueryStructs(db, &result, "SELECT * FROM market_retrieve WHERE retrieve_time<? AND active=1", invalidTime); err != nil {
		return nil, errors.As(err, invalidTime)
	}
	return result, nil
}

func SetMarketRetrieveExpire(sid string, active bool) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE market_retrieve SET active=? WHERE sid=?", active, sid); err != nil {
		return errors.As(err, sid)
	}
	// TODO: release the unsealed storage?
	return nil
}

func GetMarketRetrieveExpires(invalidTime time.Time) ([]storiface.SectorFile, error) {
	exireMarketRetrieveLock.Lock()
	defer exireMarketRetrieveLock.Unlock()

	db := GetDB()
	rows, err := db.Query(`
SELECT
    tb1.sid, tb3.id,tb3.mount_dir,tb3.mount_type, tb4.id,tb4.mount_dir,tb4.mount_type
FROM
	market_retrieve tb1
	INNER JOIN sector_info tb2 ON tb1.sid=tb2.id
	LEFT JOIN storage_info tb3 ON tb2.storage_unsealed=tb3.id
	LEFT JOIN storage_info tb4 ON tb2.storage_sealed=tb3.id
WHERE
	tb1.retrieve_time<? AND tb1.active=1
	`, invalidTime)
	if err != nil {
		return nil, errors.As(err, invalidTime)
	}
	defer database.Close(rows)

	result := []storiface.SectorFile{}
	for rows.Next() {
		sid := ""
		unsealedStorage := sql.NullInt64{}
		unsealedDir := sql.NullString{}
		unsealedKind := sql.NullString{}
		sealedStorage := sql.NullInt64{}
		sealedDir := sql.NullString{}
		sealedKind := sql.NullString{}
		if err := rows.Scan(
			&sid,
			&unsealedStorage,
			&unsealedDir,
			&unsealedKind,
			&sealedStorage,
			&sealedDir,
			&sealedKind,
		); err != nil {
			return nil, errors.As(err, invalidTime)
		}
		sFile := storiface.SectorFile{
			SectorId: sid,
		}
		if unsealedDir.Valid {
			sFile.UnsealedRepo = filepath.Join(unsealedDir.String, strconv.FormatInt(unsealedStorage.Int64, 10))
			sFile.UnsealedStorageId = unsealedStorage.Int64
			sFile.UnsealedStorageType = unsealedKind.String
			sFile.IsMarketSector = true
		}
		if sealedDir.Valid {
			sFile.SealedRepo = filepath.Join(sealedDir.String, strconv.FormatInt(sealedStorage.Int64, 10))
			sFile.SealedStorageId = sealedStorage.Int64
			sFile.SealedStorageType = sealedKind.String
		}
		result = append(result, sFile)
	}
	return result, nil
}
