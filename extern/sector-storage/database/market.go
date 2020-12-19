package database

import (
	"time"

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

func ExpireMarketRetrieve(invalidTime time.Time) ([]MarketRetrieve, error) {
	result := []MarketRetrieve{}
	db := GetDB()
	if err := database.QueryStructs(db, &result, "SELECT * FROM market_retrieve WHERE retrieve_time<? AND active=1", invalidTime); err != nil {
		return nil, errors.As(err, invalidTime)
	}
	if _, err := db.Exec("UPDATE market_retrieve SET active=0 WHERE retrieve_time<? AND active=1", invalidTime); err != nil {
		return nil, errors.As(err, invalidTime)
	}
	return result, nil
}
