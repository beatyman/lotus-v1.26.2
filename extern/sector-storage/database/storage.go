package database

import (
	"database/sql"
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

const (
	STORAGE_KIND_SEALED   = 0
	STORAGE_KIND_UNSEALED = 1
)

type StorageInfo struct {
	ID             int64     `db:"id,auto_increment"`
	UpdateTime     time.Time `db:"updated_at"`
	Kind           int       `db:"kind"`
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
func AddStorageInfo(info *StorageInfo) (int64, error) {
	if info.Version == 0 {
		info.Version = time.Now().UnixNano()
	}
	info.UpdateTime = time.Now()

	db := GetDB()
	if _, err := database.InsertStruct(db, info, "storage_info"); err != nil {
		return 0, errors.As(err, *info)
	}
	return info.ID, nil
}

func GetStorageInfo(id int64) (*StorageInfo, error) {
	db := GetDB()
	info := &StorageInfo{}
	if err := database.QueryStruct(db, info, "SELECT * FROM storage_info WHERE id=?", id); err != nil {
		return nil, errors.As(err, id)
	}
	return info, nil
}

func DisableStorage(id int64, disable bool) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET disable=?,ver=? WHERE id=?", disable, time.Now().UnixNano(), id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func UpdateStorageInfo(info *StorageInfo) error {
	db := GetDB()
	info.UpdateTime = time.Now()
	if _, err := db.Exec(`
UPDATE
	storage_info 
SET
	updated_at=?,
	max_size=?,keep_size=?,
	max_work=?, 
	mount_type=?,mount_signal_uri=?,mount_transf_uri=?,
	mount_dir=?,mount_opt=?,
	ver=?
WHERE
	id=?
	`,
		time.Now(),
		// no cur_work and used_size because it maybe in locking.
		info.MaxSize, info.KeepSize,
		info.MaxWork,
		info.MountType, info.MountSignalUri, info.MountTransfUri,
		info.MountDir, info.MountOpt,
		info.Version,
		info.ID,
	); err != nil {
		return errors.As(err, info)
	}
	return nil
}

func GetAllStorageInfo() ([]StorageInfo, error) {
	list := []StorageInfo{}
	db := GetDB()
	if err := database.QueryStructs(db, &list, "SELECT * FROM storage_info WHERE disable=0"); err != nil {
		return nil, errors.As(err, "all")
	}
	return list, nil
}
func SearchStorageInfoBySignalIp(ip string) ([]StorageInfo, error) {
	db := GetDB()
	list := []StorageInfo{}
	if err := database.QueryStructs(db, &list, "SELECT * FROM storage_info WHERE mount_signal_uri like ?", ip+"%"); err != nil {
		return nil, errors.As(err, ip)
	}
	return list, nil
}

func StorageMaxVer() (int64, error) {
	db := GetDB()
	ver := sql.NullInt64{}
	if err := database.QueryElem(db, &ver, "SELECT max(ver) FROM storage_info"); err != nil {
		return 0, errors.As(err)
	}
	return ver.Int64, nil
}

// max(ver) is the compare key.
func ChecksumStorage(sumVer int64) ([]StorageInfo, error) {
	db := GetDB()
	ver := sql.NullInt64{}
	if err := database.QueryElem(db, &ver, "SELECT max(ver) FROM storage_info"); err != nil {
		return nil, errors.As(err, sumVer)
	}
	if ver.Int64 == sumVer {
		return []StorageInfo{}, nil
	}

	// return all if version not match
	return GetAllStorageInfo()
}

// SPEC: id ==0 will return all storage node
func GetStorageCheck(id int64) (StorageStatusSort, error) {
	mdb := GetDB()
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

	list := StorageStatusSort{}
	for rows.Next() {
		stat := StorageStatus{}
		if err := rows.Scan(
			&stat.StorageId,
			&stat.MountDir,
			&stat.MountUri,
			&stat.Disable,
		); err != nil {
			return nil, errors.As(err)
		}
		list = append(list, stat)
	}
	if len(list) == 0 {
		return nil, errors.ErrNoData.As(id)
	}
	return list, nil
}
