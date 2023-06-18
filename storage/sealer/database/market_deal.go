package database

import (
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type MarketDealInfo struct {
	ID          string    `db:"id"` // propose id
	UpdatedAt   time.Time `db:"updated_at"`
	RootCid     string    `db:"root_cid"`
	PieceCid    string    `db:"piece_cid"`
	PieceSize   int64     `db:"piece_size"`
	ClientAddr  string    `db:"client_addr"`
	FileLocal   string    `db:"file_local"`
	FileRemote  string    `db:"file_remote"`
	FileStorage int64     `db:"file_storage"`

	// pack sector in
	Sid     string `db:"sid"` // maybe is empty
	DealNum int64  `db:"deal_num"`
	Offset  int64  `db:"offset"`
	State   int    `db:"state,0"`
}

// support db, tx
func AddMarketDealWithExec(exec database.Execer, info *MarketDealInfo) error {
	if _, err := database.InsertStruct(exec, info, "market_deal"); err != nil {
		return errors.As(err)
	}
	return nil
}

func AddMarketDealInfo(info *MarketDealInfo) error {
	mdb := GetDB()
	return AddMarketDealWithExec(mdb, info)
}

func GetMarketDealInfo(propID string) (*MarketDealInfo, error) {
	mdb := GetDB()
	info := &MarketDealInfo{}
	if err := database.QueryStruct(mdb, info, "SELECT * FROM market_deal WHERE id=?", propID); err != nil {
		return nil, errors.As(err, propID)
	}
	return info, nil
}

func GetMarketDealInfoBySid(sid string) ([]MarketDealInfo, error) {
	mdb := GetDB()
	info := []MarketDealInfo{}
	if err := database.QueryStructs(mdb, &info,
		"SELECT * FROM market_deal WHERE sid=?",
		sid,
	); err != nil {
		return nil, errors.As(err, sid)
	}
	return info, nil
}
func ListMarketDealInfo(beginTime, endTime time.Time, state int) ([]MarketDealInfo, error) {
	mdb := GetDB()
	info := []MarketDealInfo{}
	if err := database.QueryStructs(mdb, &info,
		"SELECT * FROM market_deal WHERE created_at BETWEEN ? AND ? AND state=?",
		beginTime, endTime, state,
	); err != nil {
		return nil, errors.As(err, beginTime, endTime, state)
	}
	return info, nil
}

func SetMarketDealState(propID string, state int) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE market_deal SET state=?,updated_at=? WHERE id=?", state, time.Now(), propID); err != nil {
		return errors.As(err)
	}
	return nil
}

// only update the sector pack information.
func UpdateMarketDeal(info *MarketDealInfo) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
  market_deal
SET
  updated_at=?,
  sid=?,
  deal_num=?,
  offset=?
WHERE
  id=?`,
		time.Now(),
		info.Sid, info.DealNum, info.Offset,
		info.ID,
	); err != nil {
		return errors.As(err)
	}
	return nil
}
