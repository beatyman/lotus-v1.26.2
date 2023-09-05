package database

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

const (
	STORAGE_KIND_SEALED   = 0
	STORAGE_KIND_UNSEALED = 1
	STORAGE_KIND_MARKET   = 10
)

const (
	MOUNT_TYPE_NFS    = "nfs"
	MOUNT_TYPE_GFS    = "glusterfs"
	MOUNT_TYPE_HLM    = "hlm-storage"
	MOUNT_TYPE_CUSTOM = "custom"
	MOUNT_TYPE_CEPH   = "ceph"
	MOUNT_TYPE_OSS    = "oss"
	MOUNT_TYPE_PB     = "pb-storage"    // the auth save with params url and will set to env
	MOUNT_TYPE_FCFS   = "fcfs"  //七牛文件存储
	MOUNT_TYPE_UFILE  = "ufile" //US3文件存储,miner端使用挂载模式,worker端使用对象存储SDK上传下载
)

type StorageMountPoint struct {
	MountDir  string
	MountType string
}

type StorageStatus struct {
	StorageId    int64
	Kind         int
	MountType    string
	MountDir     string
	MountUri     string
	MountAuthUri string
	Disable      bool
	Used         time.Duration
	Err          string
}
type StorageStatusSort []StorageStatus

func (g StorageStatusSort) Len() int {
	return len(g)
}
func (g StorageStatusSort) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}
func (g StorageStatusSort) Less(i, j int) bool {
	return g[i].Used < g[j].Used
}

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
	MountAuthUri   string    `db:"mount_auth_uri"`
	Version        int64     `db:"ver"`
	MountAuth      string    `db:"mount_auth"`
}
type StorageAuth struct {
	StorageInfo
	//MountAuth string `db:"mount_auth"` // TODO: make auth scope
}

func (s *StorageInfo) SetLastInsertId(id int64, err error) {
	if err != nil {
		panic(err)
	}
	s.ID = id
}
func AddStorage(info *StorageAuth) (int64, error) {
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
func GetStorage(id int64) (*StorageAuth, error) {
	db := GetDB()
	info := &StorageAuth{}
	if err := database.QueryStruct(db, info, "SELECT * FROM storage_info WHERE id=?", id); err != nil {
		return nil, errors.As(err, id)
	}
	return info, nil
}

func GetStorageAuth(id int64) (string, error) {
	db := GetDB()
	auth := ""
	if err := database.QueryElem(db, &auth, "SELECT mount_auth FROM storage_info WHERE id=?", id); err != nil {
		return "", errors.As(err, id)
	}
	return auth, nil
}

func DisableStorage(id int64, disable bool) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE storage_info SET disable=?,ver=? WHERE id=?", disable, time.Now().UnixNano(), id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func UpdateStorage(info *StorageAuth) error {
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
	mount_dir=?,mount_opt=?,mount_auth=?,mount_auth_uri=?,
	ver=?
WHERE
	id=?
	`,
		time.Now(),
		// no cur_work and used_size because it maybe in locking.
		info.MaxSize, info.KeepSize,
		info.MaxWork,
		info.MountType, info.MountSignalUri, info.MountTransfUri,
		info.MountDir, info.MountOpt, info.MountAuth, info.MountAuthUri,
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
	if err := database.QueryStructs(db, &list, "SELECT * FROM storage_info WHERE mount_transf_uri like ?", ip+"%"); err != nil {
		return nil, errors.As(err, ip)
	}
	return list, nil
}
func GetStorageAuthByUri(uri string) ([]StorageAuth, error) {
	db := GetDB()
	info := []StorageAuth{}
	if err := database.QueryStructs(db, &info, "SELECT * FROM storage_info WHERE mount_auth_uri=?", uri); err != nil {
		return nil, errors.As(err, uri)
	}
	return info, nil
}

// need remount all the mounted afeter change the auth.
func ChangeSealedStorageAuth(ctx context.Context) error {
	db := GetDB()
	rows, err := db.Query("SELECT mount_auth_uri FROM storage_info WHERE mount_type='hlm-storage' AND disable=0 AND kind=0")
	if err != nil {
		return errors.As(err)
	}
	auths := map[string]bool{}
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			rows.Close()
			return errors.As(err)
		}
		auths[uri] = true
	}
	rows.Close()

	for uri, _ := range auths {
		if _, err := ChangeHlmStorageAuth(ctx, uri); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

// change the storage auth, and return who have affected.
func ChangeHlmStorageAuth(ctx context.Context, authUri string) ([]StorageAuth, error) {
	auths, err := GetStorageAuthByUri(authUri)
	if err != nil {
		return nil, errors.As(err, authUri)
	}
	if len(auths) == 0 {
		return auths, nil
	}

	data, err := hlmclient.NewAuthClient(auths[0].MountAuthUri, auths[0].MountAuth).ChangeAuth(ctx)
	if err != nil {
		return nil, errors.As(err, authUri)
	}
	mountOpt := fmt.Sprintf("%x", md5.Sum([]byte(string(data)+"read")))
	ver := time.Now().UnixNano()

	db := GetDB()
	if _, err := db.Exec(
		"UPDATE storage_info set mount_auth=?,mount_opt=?,ver=? WHERE mount_auth_uri=?",
		string(data), mountOpt,
		ver,
		authUri,
	); err != nil {
		return nil, errors.As(err)
	}
	// update the result.
	for i, _ := range auths {
		auths[i].MountAuth = string(data)
		auths[i].MountOpt = mountOpt
		auths[i].Version = ver
	}
	return auths, nil
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
			"SELECT tb1.id, tb1.kind, tb1.mount_type, tb1.mount_dir, tb1.mount_signal_uri, tb1.mount_auth_uri, disable FROM storage_info tb1 WHERE tb1.id=?",
			id,
		)
		if err != nil {
			return nil, errors.As(err)
		}
	} else {
		rows, err = mdb.Query(
			"SELECT tb1.id, tb1.kind, tb1.mount_type, tb1.mount_dir, tb1.mount_signal_uri, tb1.mount_auth_uri, disable FROM storage_info tb1 LIMIT 10000", // TODO: more then 10000
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
			&stat.Kind,
			&stat.MountType,
			&stat.MountDir,
			&stat.MountUri,
			&stat.MountAuthUri,
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
func GetMarketStorageInfo() (*StorageInfo, error) {
	db := GetDB()
	info := &StorageInfo{}
	if err := database.QueryStruct(db, info, "SELECT * FROM storage_info WHERE disable = 0 and kind = ? limit 1", STORAGE_KIND_MARKET); err != nil {
		return nil, errors.As(err)
	}
	return info, nil
}
func GetAllStorageInfoAll() ([]StorageInfo, error) {
	list := []StorageInfo{}
	db := GetDB()
	if err := database.QueryStructs(db, &list, "SELECT * FROM storage_info "); err != nil {
		return nil, errors.As(err, "all")
	}
	return list, nil
}
func rebuildSectorFromSealedStorage(tx *sql.Tx, sid string, sealedStorage int64) (*SectorInfo, error) {
	sectorID, err := storiface.ParseSectorID(sid)
	if err != nil {
		return nil, errors.As(err)
	}
	info := &SectorInfo{
		ID: sid,
	}
	found := false
	if err := database.QueryStruct(tx, info, "SELECT * FROM sector_info WHERE id=?", sid); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		// data not found
		// rebuild sector info
	} else {
		found = true
	}
	if info.StorageSealed > 0 {
		return info, nil
	}
	now := time.Now()
	info.MinerId = storiface.MinerID(sectorID.Miner)
	info.StorageSealed = sealedStorage
	info.State = 200
	info.StateTime = now
	info.UpdateTime = now
	info.CreateTime = now
	if !found {
		if _, err := database.InsertStruct(tx, info, "sector_info"); err != nil {
			return info, errors.As(err, sid)
		}
	} else {
		if _, err := database.Exec(tx, "UPDATE sector_info SET storage_sealed=? WHERE id=?", info.StorageSealed, info.ID); err != nil {
			return info, errors.As(err, sid)
		}
	}

	log.Infof("Rebuild sector sealed:%s,%d", info.ID, info.StorageSealed)
	return info, nil
}

func rebuildSectorFromUnsealedStorage(tx *sql.Tx, sid string, unsealedStorage int64) (*SectorInfo, error) {
	sectorID, err := storiface.ParseSectorID(sid)
	if err != nil {
		return nil, errors.As(err)
	}
	info := &SectorInfo{
		ID: sid,
	}
	found := false
	if err := database.QueryStruct(tx, info, "SELECT * FROM sector_info WHERE id=?", sid); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		// data not found
		// rebuild sector info
	} else {
		found = true
	}

	now := time.Now()
	info.MinerId = storiface.MinerID(sectorID.Miner)
	info.StorageUnsealed = unsealedStorage
	info.UpdateTime = now
	info.CreateTime = now
	if !found {
		if _, err := database.InsertStruct(tx, info, "sector_info"); err != nil {
			return info, errors.As(err, sid)
		}
	} else {
		if _, err := database.Exec(tx, "UPDATE sector_info SET storage_unsealed=? WHERE id=?", info.StorageUnsealed, info.ID); err != nil {
			return info, errors.As(err, sid)
		}
	}

	log.Infof("Rebuild sector unsealed:%s,%d", info.ID, info.StorageUnsealed)
	return info, nil
}

func ForceRebuildSector(id string) (*SectorInfo, error) {
	// data not found, search the storage
	sealedStorageInfo, err := SearchSectorFromSealedStorage(id)
	if err != nil {
		return nil, errors.As(err)
	}
	unsealedStorageInfo, _ := SearchSectorFromUnsealedStorage(id)

	tx, err := mdb.Begin()
	if err != nil {
		return nil, errors.As(err)
	}

	// rebuild sector info
	info, err := rebuildSectorFromSealedStorage(tx, id, sealedStorageInfo.ID)
	if err != nil {
		database.Rollback(tx)
		return nil, errors.As(err)
	}
	if unsealedStorageInfo != nil {
		info, err = rebuildSectorFromUnsealedStorage(tx, id, unsealedStorageInfo.ID)
		if err != nil {
			database.Rollback(tx)
			return nil, errors.As(err)
		}
	}
	if err := tx.Commit(); err != nil {
		database.Rollback(tx)
		return nil, errors.As(err)
	}
	return info, nil
}
func RebuildSectorFromStorage(ctx context.Context, storageId int64,minerAddr string) error {
	mi, err := address.NewFromString(minerAddr)
	if err != nil {
		return errors.As(err)
	}
	mid, err := address.IDFromAddress(mi)
	if err != nil {
		return errors.As(err)
	}
	rows, err := mdb.Query("SELECT id,mount_dir,kind FROM storage_info")
	if err != nil {
		return errors.As(err)
	}
	defer rows.Close()

	sealedPaths := map[int64]string{}
	unsealedPaths := map[int64]string{}
	for rows.Next() {
		id := int64(0)
		mountDir := ""
		kind := 0
		if err := rows.Scan(&id, &mountDir, &kind); err != nil {
			return errors.As(err)
		}
		if storageId > 0 && storageId != id {
			continue
		}
		switch kind {
		case 0:
			sealedPaths[id] = filepath.Join(mountDir, strconv.FormatInt(id, 10), "sealed")
		case 1:
			unsealedPaths[id] = filepath.Join(mountDir, strconv.FormatInt(id, 10), "unsealed")
		}
	}
	tx, err := mdb.Begin()
	if err != nil {
		return errors.As(err)
	}
	for sealedId, sealedPath := range sealedPaths {
		// read sector name
		files, err := fsutil.ReadDir(sealedPath)
		if err != nil {
			database.Rollback(tx)
			return errors.As(err, sealedId, sealedPath)
		}
		for _, f := range files {
			sid := f.Name()
			if strings.Contains(sid, strconv.Itoa(int(mid))) {
				if _, err := rebuildSectorFromSealedStorage(tx, sid, sealedId); err != nil {
					log.Warn(errors.As(err, sealedPath))
					continue
				}
			}
		}
	}
	for unsealedId, unsealedPath := range unsealedPaths {
		// read sector name
		files, err := fsutil.ReadDir(unsealedPath)
		if err != nil {
			database.Rollback(tx)
			return errors.As(err, unsealedId, unsealedPath)
		}
		for _, f := range files {
			sid := f.Name()
			if strings.Contains(sid, strconv.Itoa(int(mid))) {
				if _, err := rebuildSectorFromUnsealedStorage(tx, sid, unsealedId); err != nil {
					log.Warn(errors.As(err, unsealedPath))
					continue
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		database.Rollback(tx)
		return errors.As(err)
	}
	return nil
}
func SearchSectorFromSealedStorage(sectorID string) (*StorageInfo, error) {
	storages := []StorageInfo{}
	err := database.QueryStructs(mdb, &storages, "SELECT * FROM storage_info WHERE kind=0 ORDER BY id DESC")
	if err != nil {
		return nil, errors.As(err)
	}
	for _, st := range storages {
		path := filepath.Join(st.MountDir, strconv.FormatInt(st.ID, 10), "sealed", sectorID)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Warn(errors.As(err))
			continue
		}
		// found
		return &st, nil
	}
	return nil, errors.ErrNoData.As(sectorID)
}
func SearchSectorFromUnsealedStorage(sectorID string) (*StorageInfo, error) {
	storages := []StorageInfo{}
	err := database.QueryStructs(mdb, &storages, "SELECT * FROM storage_info WHERE kind=1 ORDER BY id DESC")
	if err != nil {
		return nil, errors.As(err)
	}
	for _, st := range storages {
		path := filepath.Join(st.MountDir, strconv.FormatInt(st.ID, 10), "unsealed", sectorID)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Warn(errors.As(err))
			continue
		}
		// found
		return &st, nil
	}
	return nil, errors.ErrNoData.As(sectorID)
}
