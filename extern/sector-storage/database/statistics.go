package database

import (
	"database/sql"
	"time"

	"github.com/gwaylib/errors"
)

type StatisWin struct {
	Id      string
	WinAll  int
	WinErr  int
	WinGen  int
	WinSuc  int
	WinExp  int
	WinUsed int64
}

func AddWinTimes(id string, exp int) error {
	mdb := GetDB()
	result, err := mdb.Exec("UPDATE statis_win SET win_all=win_all+1,win_exp=? WHERE id=?", exp, id)
	if err != nil {
		return errors.As(err, id, exp)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.As(err, id, exp)
	}
	if affected > 0 {
		return nil
	}

	// row not exist, make a new record.
	if _, err := mdb.Exec("INSERT INTO statis_win(id,win_all,win_exp)VALUES(?,?,?)", id, 1, exp); err != nil {
		return errors.As(err, id)
	}
	return nil
}
func AddWinErr(id string) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE statis_win SET win_err=win_err+1 WHERE id=?", id); err != nil {
		return errors.As(err, id)
	}
	return nil
}
func AddWinGen(id string, used time.Duration) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE statis_win SET win_gen=win_gen+1, win_used=win_used+? WHERE id=?", used, id); err != nil {
		return errors.As(err, id, used)
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
	if err := mdb.QueryRow("SELECT win_all, win_err, win_gen, win_suc, win_exp, win_used FROM statis_win WHERE id=?", id).Scan(
		&s.WinAll, &s.WinErr, &s.WinGen, &s.WinSuc, &s.WinExp, &s.WinUsed,
	); err != nil {
		if err != sql.ErrNoRows {
			return s, err
		}
	}
	return s, nil
}
