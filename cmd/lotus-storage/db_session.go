package main

import (
	"database/sql"

	"github.com/gwaylib/errors"
)

func GetPath(key string) (string, string, error) {
	db := GetDB()
	var auth, path string
	if err := db.QueryRow("SELECT auth, path FROM nfs_session WHERE id=?", key).Scan(&auth, &path); err != nil {
		if sql.ErrNoRows == err {
			return "", "", errors.ErrNoData.As(key)
		}
		return "", "", errors.As(err)
	}
	return auth, path, nil
}

func AddPath(key string, auth, path string) error {
	db := GetDB()
	if _, err := db.Exec("INSERT INTO nfs_session(id,auth,path)VALUES(?,?,?)", key, auth, path); err != nil {
		return errors.As(err, key, path)
	}
	return nil
}

func CleanPath() error {
	db := GetDB()
	if _, err := db.Exec("DELETE FROM nfs_session"); err != nil {
		return errors.As(err)
	}
	return nil
}
