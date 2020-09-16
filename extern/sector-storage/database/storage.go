package database

import (
	"database/sql"
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type StorageInfo struct {
	ID             int64     `db:"id,auto_increment"`
	UpdateTime     time.Time `db:"updated_at"`
	MaxSize        int64     `db:"max_size"`
	KeepSize       int64     `db:"keep_size"`
	UsedSize       int64     `db:"used_size"`
	SectorSize     int64     `db:"sector_size"`
	MaxWork        int       `db:"max_work"`
	CurWork        int       `db:"cur_work"`
	MountType      string    `db:"mount_type"`
	MountSignalUri string    `db:"mount_signal_uri"`
	MountTransfUri string    `db:"mount_transf_uri"`
	MountDir       string    `db:"mount_dir"`
	MountOpt       string    `db:"mount_opt"`
	Version        int64     `db:"ver"`
}

func (s *StorageInfo) SetLastInsertId(id int64, err error) {
	if err != nil {
		panic(err)
	}
	s.ID = id
}
func AddStorageInfo(info *StorageInfo) error {
	if info.Version == 0 {
		info.Version = time.Now().UnixNano()
	}
	db := GetDB()
	if _, err := database.InsertStruct(db, info, "storage_info"); err != nil {
		return errors.As(err, *info)
	}
	return nil
}
func GetStorageInfo(id int64) (*StorageInfo, error) {
	db := GetDB()
	info := &StorageInfo{}
	if err := database.QueryStruct(db, info, "SELECT * FROM storage_info WHERE id=?", id); err != nil {
		return nil, errors.As(err, id)
	}
	return info, nil
}

func UpdateStorageInfo(info *StorageInfo) error {
	db := GetDB()
	// TODO: more update?
	if _, err := db.Exec(
		"UPDATE storage_info SET max_work=?, max_size=? WHERE id=?",
		info.MaxWork, info.MaxSize, info.ID,
	); err != nil {
		return errors.As(err, info)
	}
	return nil
}

type StorageList []StorageInfo

func GetAllStorageInfo() (StorageList, error) {
	list := StorageList{}
	db := GetDB()
	if err := database.QueryStructs(db, &list, "SELECT * FROM storage_info where disable=0"); err != nil {
		return nil, errors.As(err, "all")
	}
	return list, nil
}

func ChecksumStorage(sumVer int64) (StorageList, error) {
	db := GetDB()
	ver := sql.NullInt64{}
	if err := database.QueryElem(db, &ver, "SELECT max(ver) FROM storage_info"); err != nil {
		return nil, errors.As(err, sumVer)
	}
	if ver.Int64 == sumVer {
		return StorageList{}, nil
	}

	// return all if version not match
	return GetAllStorageInfo()
}

func GetStorageCheck(id int64) (StorageStatusSort, error) {
	mdb := GetDB()
	list := StorageStatusSort{}
	var rows *sql.Rows
	var err error
	if id > 0 {
		rows, err = mdb.Query(
			"SELECT tb1.id, tb1.mount_dir, tb1.mount_signal_uri, disable FROM storage_info tb1 WHERE tb1.id=?",
			id,
		)
		if err != nil {
			return nil, errors.As(err)
		}
	} else {
		rows, err = mdb.Query(
			"SELECT tb1.id, tb1.mount_dir, tb1.mount_signal_uri, disable FROM storage_info tb1 LIMIT 10000", // TODO: more then 10000
		)
		if err != nil {
			return nil, errors.As(err)
		}
	}
	defer rows.Close()

	for rows.Next() {
		state := StorageStatus{}
		if err := rows.Scan(
			&state.StorageId,
			&state.MountDir,
			&state.MountUri,
			&state.Disable,
		); err != nil {
			return nil, errors.As(err)
		}
	}
	if len(list) == 0 {
		return nil, errors.ErrNoData
	}
	return list, nil
}
