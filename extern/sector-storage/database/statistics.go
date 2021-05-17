package database

import (
	"database/sql"

	"github.com/gwaylib/errors"
)

func AddWinTimes(id string) error {
	mdb := GetDB()
	result, err := mdb.Exec("UPDATE statis_win SET win_all=win_all+1 WHERE id=?", id)
	if err != nil {
		return errors.As(err, id)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.As(err, id)
	}
	if affected > 0 {
		return nil
	}

	// row not exist, make a new record.
	if _, err := mdb.Exec("INSERT INTO statis_win(id,win_all)VALUES(?,?)", id, 1); err != nil {
		return errors.As(err, id)
	}
	return nil
}
func AddWinSuc(id string) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE statis_win SET win_suc=win_suc+1 WHERE id=?", id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func StatisWinSuc(id string) (int, int, error) {
	var all, suc int
	mdb := GetDB()
	if err := mdb.QueryRow("SELECT win_all, win_suc FROM statis_win WHERE id=?", id).Scan(&all, &suc); err != nil {
		if err != sql.ErrNoRows {
			return 0, 0, err
		}
	}
	return all, suc, nil
}
