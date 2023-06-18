package database

import (
	"fmt"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gwaylib/database"
)

func init() {
	database.REFLECT_DRV_NAME = database.DRV_NAME_SQLITE3
}

var (
	mdb        *database.DB
	mdblk      sync.Mutex
	dbGlobalLk sync.Mutex
)

func InitDB(repo string) {
	mdblk.Lock()
	defer mdblk.Unlock()
	if mdb != nil {
		return
	}

	log.Infof("Init sqlite db at:%s", repo)
	db, err := database.Open(database.DRV_NAME_SQLITE3, fmt.Sprintf("file:%s?_loc=auto&_mode=rwc&_journal_mode=WAL&cache=shared&encoding=UTF-8&_timeout=10000", filepath.Join(repo, "storage.db")))
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1) // more than one should happen:database is locked

	mdb = db

	// Init tables
	if _, err := db.Exec(tb_sector_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_sector_snap_sql); err != nil {
		fmt.Println(err.Error())
	}
	if _, err := db.Exec(tb_sector_rebuild_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_storage_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_worker_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_market_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_statis_win_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_market_deal_sql); err != nil {
		panic(err)
	}
	go initSectorFileCache(repo)
}

func HasDB() bool {
	mdblk.Lock()
	defer mdblk.Unlock()
	return mdb != nil
}

func GetDB() *database.DB {
	mdblk.Lock()
	defer mdblk.Unlock()
	if mdb == nil {
		panic("Need InitDB at first")
	}
	return mdb
}
