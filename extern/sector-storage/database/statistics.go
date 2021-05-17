package database

import (
	"database/sql"

	"github.com/gwaylib/errors"
)

type StatisWin struct {
	Id     string
	WinAll int
	WinGen int
	WinSuc int
}

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
func AddWinGen(id string) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE statis_win SET win_gen=win_suc+1 WHERE id=?", id); err != nil {
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

func GetStatisWin(id string) (*StatisWin, error) {
	s := &StatisWin{Id: id}
	mdb := GetDB()
	if err := mdb.QueryRow("SELECT win_all, win_gen, win_suc FROM statis_win WHERE id=?", id).Scan(&s.WinAll, &s.WinGen, &s.WinSuc); err != nil {
		if err != sql.ErrNoRows {
			return s, err
		}
	}
	return s, nil
}
