package database

import (
	"database/sql"
	"os"
	"path/filepath"
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
	mdb := GetDB()
	info := &MarketRetrieve{}
	if err := database.QueryStruct(mdb, info, "SELECT * FROM market_retrieve WHERE sid=?", sid); err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}

func AddMarketRetrieve(sid string) error {
	tx, err := GetDB().Begin()
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
		if _, err := tx.Exec("UPDATE market_retrieve SET active=1, retrieve_times=retrieve_times+1, retrieve_time=? WHERE sid=?", time.Now(), sid); err != nil {
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
	result := []MarketRetrieve{}
	if err := database.QueryStructs(GetDB(), &result, "SELECT * FROM market_retrieve WHERE retrieve_time<? AND active=1", invalidTime); err != nil {
		return nil, errors.As(err, invalidTime)
	}
	return result, nil
}

func ExpireMarketRetrieve(sid string) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE market_retrieve SET active=0 WHERE sid=?", sid); err != nil {
		return errors.As(err, sid)
	}
	return nil
}
func ExpireAllMarketRetrieve(invalidTime time.Time, minerRepo string) ([]storage.SectorFile, error) {
	db := GetDB()
	rows, err := db.Query(`
SELECT tb1.sid, tb3.mount_signal_uri,tb4.mount_signal_uri
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
		unsealed := sql.NullString{}
		sealed := sql.NullString{}
		if err := rows.Scan(
			&sid,
			&unsealed,
			&sealed,
		); err != nil {
			return nil, errors.As(err, invalidTime)
		}
		result = append(result, storage.SectorFile{
			SectorId:     sid,
			SealedRepo:   sealed.String,
			UnsealedRepo: unsealed.String,
		})
	}

	executed := []storage.SectorFile{}
	for _, sFile := range result {
		if len(sFile.UnsealedRepo) > 0 {
			log.Warnf("remove unseal", sFile.UnsealedFile())
			err = os.RemoveAll(sFile.UnsealedFile())
			if err != nil {
				break
			}
		}
		if len(minerRepo) > 0 {
			file := filepath.Join(minerRepo, "unsealed", sFile.SectorId)
			log.Warnf("remove unseal", file)
			err = os.RemoveAll(file)
			if err != nil {
				break
			}
		}
		_, err = db.Exec("UPDATE market_retrieve SET active=0 WHERE sid", sFile.SectorId)
		if err != nil {
			break
		}
		executed = append(executed, sFile)
	}
	return executed, err
}
