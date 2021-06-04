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

func AddWinTimes(submitTime time.Time, exp int) error {
	mdb := GetDB()
	now := submitTime.UTC()
	id := now.Format("20060102")
	result, err := mdb.Exec("UPDATE statis_win SET updated_at=?,win_all=win_all+1,win_exp=? WHERE id=?", now, exp, id)
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
	if _, err := mdb.Exec("INSERT INTO statis_win(id,created_at,updated_at,win_all,win_exp)VALUES(?,?,?,?,?)", id, now, now, 1, exp); err != nil {
		return errors.As(err, id)
	}
	return nil
}
func AddWinErr(submitTime time.Time) error {
	mdb := GetDB()
	id := submitTime.UTC().Format("20060102")
	if _, err := mdb.Exec("UPDATE statis_win SET win_err=win_err+1 WHERE id=?", id); err != nil {
		return errors.As(err, id)
	}
	return nil
}
func AddWinGen(submitTime time.Time, used time.Duration) error {
	mdb := GetDB()
	id := submitTime.UTC().Format("20060102")
	if _, err := mdb.Exec("UPDATE statis_win SET win_gen=win_gen+1, win_used=win_used+? WHERE id=?", used, id); err != nil {
		return errors.As(err, id, used)
	}
	return nil
}
func AddWinSuc(submitTime time.Time) error {
	mdb := GetDB()
	id := submitTime.UTC().Format("20060102")
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
func GetStatisWins(now time.Time, limit int) ([]StatisWin, error) {
	mdb := GetDB()
	maxId := now.Add(24 * time.Hour).UTC().Format("20060102")
	rows, err := mdb.Query("SELECT id, win_all, win_err, win_gen, win_suc, win_exp, win_used FROM statis_win WHERE id<? ORDER BY id DESC limit ?", maxId, limit)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rows.Close()
	result := []StatisWin{}
	for rows.Next() {
		s := StatisWin{}
		if err := rows.Scan(
			&s.Id, &s.WinAll, &s.WinErr, &s.WinGen, &s.WinSuc, &s.WinExp, &s.WinUsed,
		); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}
