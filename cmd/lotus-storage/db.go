package main

import (
	"fmt"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	database.REFLECT_DRV_NAME = database.DRV_NAME_SQLITE3
}

var (
	mdb        *database.DB
	mdblk      sync.Mutex
	dbGlobalLk sync.Mutex
)

func InitDB(repo string) error {
	mdblk.Lock()
	defer mdblk.Unlock()
	if mdb != nil {
		return nil
	}

	log.Infof("Init sqlite db at:%s", repo)
	db, err := database.Open(database.DRV_NAME_SQLITE3, fmt.Sprintf("file:%s?_loc=auto&_mode=rwc&_journal_mode=WAL&cache=shared&encoding=UTF-8&_timeout=10000", filepath.Join(repo, _dbFile)))
	if err != nil {
		return errors.As(err)
	}
	db.SetMaxOpenConns(1) // more than one should happen:database is locked

	mdb = db

	// Init tables
	if _, err := db.Exec(tb_nfs_session_sql); err != nil {
		return errors.As(err)
	}
	return nil
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
		log.Fatal("Need InitDB at first")
	}
	return mdb
}
