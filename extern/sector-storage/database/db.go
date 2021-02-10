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

type DBOnceLk struct{
	Locker sync.Mutex
}

func (lk *DBOnceLk)Lock(){
	// TODO:
	//lk.Locker.Lock()
}
func (lk *DBOnceLk)Unlock(){
	// TODO:
	//lk.Locker.Unlock()
}

var (
	mdb   *database.DB
	mdblk sync.Mutex
	oneLk DBOnceLk
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
	if _, err := db.Exec(tb_storage_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_worker_sql); err != nil {
		panic(err)
	}
	if _, err := db.Exec(tb_market_sql); err != nil {
		panic(err)
	}

	go gcSectorFileCache()
}

func HasDB() bool {
	mdblk.Lock()
	defer mdblk.Unlock()
	return mdb != nil
}

func GetDB() (*database.DB, *DBOnceLk) {
	mdblk.Lock()
	defer mdblk.Unlock()
	if mdb == nil {
		panic("Need InitDB at first")
	}
	oneLk.Lock()
	return mdb, &oneLk
}
