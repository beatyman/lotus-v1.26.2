package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type MarketRetrieve struct {
	SectorId      string    `db:"sid"` // s-t01001-1
	RetrieveTimes int       `db:"retrieve_times"`
	RetrieveTime  time.Time `db:"retrieve_time"`
}

func GetMarketRetrieve(sid string) (*MarketRetrieve, error) {
	mdb, lk := GetDB()
	defer lk.Unlock()

	info := &MarketRetrieve{}
	if err := database.QueryStruct(mdb, info, "SELECT * FROM market_retrieve WHERE sid=?", sid); err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}

func AddMarketRetrieve(sid string) error {
	db, lk := GetDB()
	defer lk.Unlock()

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

func GetExpireMarketRetrieve(invalidTime time.Time) ([]MarketRetrieve, error) {
	db, lk := GetDB()
	defer lk.Unlock()

	result := []MarketRetrieve{}
	if err := database.QueryStructs(db, &result, "SELECT * FROM market_retrieve WHERE retrieve_time<? AND active=1", invalidTime); err != nil {
		return nil, errors.As(err, invalidTime)
	}
	return result, nil
}

func ExpireMarketRetrieve(sid string) error {
	db, lk := GetDB()
	defer lk.Unlock()

	if _, err := db.Exec("UPDATE market_retrieve SET active=0 WHERE sid=?", sid); err != nil {
		return errors.As(err, sid)
	}
	return nil
}

func ExpireAllMarketRetrieve(invalidTime time.Time, minerRepo string) ([]storage.SectorFile, error) {
	db, lk := GetDB()
	defer lk.Unlock()

	rows, err := db.Query(`
SELECT tb1.sid, tb3.id,tb3.mount_dir, tb4.id,tb4.mount_dir
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

	result := []storage.SectorFile{}
	for rows.Next() {
		sid := ""
		unsealedStorage := sql.NullInt64{}
		unsealedDir := sql.NullString{}
		sealedStorage := sql.NullInt64{}
		sealedDir := sql.NullString{}
		if err := rows.Scan(
			&sid,
			&unsealedStorage,
			&unsealedDir,
			&sealedStorage,
			&sealedDir,
		); err != nil {
			return nil, errors.As(err, invalidTime)
		}
		sFile := storage.SectorFile{
			SectorId: sid,
		}
		if unsealedDir.Valid {
			sFile.UnsealedRepo = filepath.Join(unsealedDir.String, strconv.FormatInt(unsealedStorage.Int64, 10))
		}
		if unsealedDir.Valid {
			sFile.SealedRepo = filepath.Join(sealedDir.String, strconv.FormatInt(sealedStorage.Int64, 10))
		}
		result = append(result, sFile)
	}

	executed := []storage.SectorFile{}
	for _, sFile := range result {
		if len(sFile.UnsealedRepo) > 0 {
			log.Warnf("remove unseal:%s", sFile.UnsealedFile())
			err = os.RemoveAll(sFile.UnsealedFile())
			if err != nil {
				break
			}
		}
		if len(minerRepo) > 0 {
			file := filepath.Join(minerRepo, "unsealed", sFile.SectorId)
			log.Warnf("remove unseal:%s", file)
			err = os.RemoveAll(file)
			if err != nil {
				break
			}
		}
		_, err = db.Exec("UPDATE market_retrieve SET updated_at=?, active=0 WHERE sid=?", time.Now(), sFile.SectorId)
		if err != nil {
			break
		}
		executed = append(executed, sFile)
	}
	return executed, err
}
