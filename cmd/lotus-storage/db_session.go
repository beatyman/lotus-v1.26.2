package main

import (
	"database/sql"

	"github.com/gwaylib/errors"
)

func GetSessionFile(key string) (string, string, string, error) {
	db := GetDB()
	var user, auth, path string
	if err := db.QueryRow("SELECT user, auth, path FROM file_session WHERE id=?", key).Scan(&user, &auth, &path); err != nil {
		if sql.ErrNoRows == err {
			return "", "", "", errors.ErrNoData.As(key)
		}
		return "", "", "", errors.As(err)
	}
	return user, auth, path, nil
}

func AddSessionFile(key string, user, auth, path string) error {
	db := GetDB()
	if _, err := db.Exec("INSERT INTO file_session(id,user,auth,path)VALUES(?,?,?,?)", key, user, auth, path); err != nil {
		return errors.As(err, key, path)
	}
	return nil
}
func DeleteSessionFile(key string) error {
	db := GetDB()
	if _, err := db.Exec("DELETE FROM file_session WHERE id=?", key); err != nil {
		return errors.As(err, key)
	}
	return nil
}

func CleanAllSessionFile() error {
	db := GetDB()
	if _, err := db.Exec("DELETE FROM file_session"); err != nil {
		return errors.As(err)
	}
	return nil
}
