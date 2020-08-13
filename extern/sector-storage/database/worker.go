package database

import (
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type WorkerInfo struct {
	ID         string    `db:"id"`
	UpdateTime time.Time `db:"updated_at"`
	Ip         string    `db:"ip"`
	SvcUri     string    `db:"svc_uri"`  // download service, it should be http://ip:port
	SvcConn    int       `db:"svc_conn"` // number of downloading connections
	Online     bool      `db:"online"`
	Disable    bool      `db:"disable"`
}

func GetWorkerInfo(id string) (*WorkerInfo, error) {
	db := GetDB()
	info := &WorkerInfo{}
	if err := database.QueryStruct(db, info, "SELECT * FROM worker_info WHERE id=?", id); err != nil {
		return nil, errors.As(err)
	}

	return info, nil
}

func OnlineWorker(info *WorkerInfo) error {
	db := GetDB()
	exist := 0
	if err := database.QueryElem(db, &exist, "SELECT count(*) FROM worker_info WHERE id=?", info.ID); err != nil {
		return errors.As(err, *info)
	}
	if exist == 0 {
		if _, err := database.InsertStruct(db, info, "worker_info"); err != nil {
			return errors.As(err, *info)
		}
		return nil
	}
	if _, err := db.Exec(
		"UPDATE worker_info SET ip=?, svc_uri=?, svc_conn=0, online=1, updated_at=? WHERE id=?",
		info.Ip, info.SvcUri, info.UpdateTime, info.ID,
	); err != nil {
		return errors.As(err, *info)
	}
	return nil
}

func OfflineWorker(id string) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET online=0,updated_at=? WHERE id=?", time.Now(), id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func DisableWorker(id string, disable bool) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET disable=?,updated_at=? WHERE id=?", disable, time.Now(), id); err != nil {
		return errors.As(err, id, disable)
	}
	return nil
}

func AddWorkerConn(id string, num int) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET svc_conn=svc_conn+? WHERE id=?", num, id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

// prepare worker connection will auto increment the connections
func PrepareWorkerConn() (*WorkerInfo, error) {
	db := GetDB()
	wInfo := &WorkerInfo{}
	mdblk.Lock()
	defer mdblk.Unlock()
	if err := database.QueryStruct(db, wInfo,
		"SELECT * FROM worker_info WHERE online=1 ORDER BY svc_conn LIMIT 1",
	); err != nil {
		return nil, errors.As(err)
	}
	if _, err := db.Exec("UPDATE worker_info SET svc_conn=svc_conn+1 WHERE id=?", wInfo.ID); err != nil {
		return nil, errors.As(err)
	}
	return wInfo, nil
}
