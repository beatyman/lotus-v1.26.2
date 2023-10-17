package database

import (
	"database/sql"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"path/filepath"
	"sync"
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

const (
	SECTOR_STATE_PLEDGE = 0

	SECTOR_STATE_MOVE = 100
	SECTOR_STATE_PUSH = 101

	SECTOR_STATE_DONE   = 200
	SECTOR_STATE_FAILED = 500
)

type SectorInfo struct {
	ID              string    `db:"id"` // s-t0101-1
	MinerId         string    `db:"miner_id"`
	UpdateTime      time.Time `db:"updated_at"`
	StorageSealed   int64     `db:"storage_sealed"`
	StorageUnsealed int64     `db:"storage_unsealed"`
	WorkerId        string    `db:"worker_id"`
	State           int       `db:"state,0"`
	Snap            int       `db:"snap,0"`
	StateTime       time.Time `db:"state_time"`
	StateTimes      int       `db:"state_times"`
	CreateTime      time.Time `db:"created_at"`
}

type SectorStorage struct {
	SectorInfo      SectorInfo
	WorkerInfo      WorkerInfo
	SealedStorage   StorageInfo
	UnsealedStorage StorageInfo
}

func AddSectorInfo(info *SectorInfo) error {
	mdb := GetDB()
	if _, err := database.InsertStruct(mdb, info, "sector_info"); err != nil {
		return errors.As(err)
	}
	return nil
}

// if data not found, it will return a 'default' WorkerId
func GetSectorInfo(id string) (*SectorInfo, error) {
	mdb := GetDB()
	info := &SectorInfo{
		ID: id,
	}
	sqlStr := "SELECT * FROM sector_info WHERE id=?"
	if err := database.QueryStruct(mdb, info, sqlStr, id); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		if rebuilt, err := ForceRebuildSector(id); err != nil {
			if !errors.ErrNoData.Equal(err) {
				return nil, errors.As(err)
			}
		} else {
			info = rebuilt
		}
	}
	return info, nil
}
func GetSectorState(id string) (int, error) {
	mdb := GetDB()
	state := -1
	if err := database.QueryElem(mdb, &state, "SELECT state FROM sector_info WHERE id=?", id); err != nil {
		return state, errors.As(err, id)
	}
	return state, nil
}

func FillSectorFile(sector storiface.SectorRef, defaultRepo string) (storiface.SectorRef, error) {
	if sector.HasRepo() {
		return sector, nil
	}
	// set to default.
	sector.SectorId = storiface.SectorName(sector.ID)
	sector.SealedRepo = defaultRepo
	sector.UnsealedRepo = defaultRepo

	if !HasDB() {
		return sector, nil
	}

	fill, err := GetSectorFile(sector.SectorId, defaultRepo)
	if err != nil {
		return sector, errors.As(err)
	}
	sector.SectorFile = *fill
	return sector, nil
}

type sectorFileCache struct {
	SectorFile storiface.SectorFile
	CreateTime time.Time
}

var (
	sectorFileCaches  = map[string]sectorFileCache{}
	sectorFileCacheLk = sync.Mutex{}
)

func initSectorFileCache(repo string) {
	// init data to memory, so it always can fetch fast.
	mdb := GetDB()
	ids := []string{}
	if err := database.QueryElems(mdb, &ids, "SELECT id FROM sector_info"); err != nil {
		log.Error(errors.As(err))
		return
	}

	start := 0
	end := 3000
	limit := 3000
	total := len(ids)
	for {
		if (start + limit) > total {
			end = total
		} else {
			end = start + limit
		}
		fetchIds := ids[start:end]
		_, err := GetSectorsFile(fetchIds, repo)
		if err != nil {
			log.Error(errors.As(err))
			return
		}
		if end >= total {
			break
		}
		start = end
	}

	sectorFileCacheLk.Lock()
	log.Infof("sector file cache loaded:%d", len(sectorFileCaches))
	sectorFileCacheLk.Unlock()
	return

	timeout := 2 * time.Minute
	tick := time.NewTicker(timeout)
	for {
		<-tick.C
		now := time.Now()
		sectorFileCacheLk.Lock()
		for key, val := range sectorFileCaches {
			if now.Sub(val.CreateTime) > timeout {
				delete(sectorFileCaches, key)
			}
		}
		log.Infof("gc db sector file:%d", len(sectorFileCaches))
		sectorFileCacheLk.Unlock()
	}
}

func GetSectorsFile(sectors []string, defaultRepo string) (map[string]storiface.SectorFile, error) {
	startTime := time.Now()
	defer func() {
		took := time.Now().Sub(startTime)
		if took > 5e9 {
			log.Warnf("GetSectorsFile took : %s", took)
		}
	}()

	defaultResult := map[string]storiface.SectorFile{}
	if !HasDB() {
		for _, sectorId := range sectors {
			defaultResult[sectorId] = storiface.SectorFile{
				SectorId:     sectorId,
				SealedRepo:   defaultRepo,
				UnsealedRepo: defaultRepo,
			}
		}
		return defaultResult, nil
	}

	dbGlobalLk.Lock()
	defer dbGlobalLk.Unlock()

	mdb := GetDB()
	// preload storage data
	storages := map[int64]StorageMountPoint{}
	rows, err := mdb.Query("SELECT id, mount_dir, mount_type FROM storage_info")
	if err != nil {
		return nil, errors.As(err, sectors)
	}
	for rows.Next() {
		id := int64(0)
		mountPoint := StorageMountPoint{}
		if err := rows.Scan(&id, &mountPoint.MountDir, &mountPoint.MountType); err != nil {
			database.Close(rows)
			return nil, errors.As(err, sectors)
		}
		storages[id] = mountPoint
	}
	database.Close(rows)

	// get sector info
	sectorStmt, err := mdb.Prepare("SELECT storage_sealed,storage_unsealed FROM sector_info WHERE id=?")
	if err != nil {
		return nil, errors.As(err, sectors)
	}
	defer sectorStmt.Close()

	result := map[string]storiface.SectorFile{}
	for _, sectorId := range sectors {
		// read from cache
		sectorFileCacheLk.Lock()
		sFile, ok := sectorFileCaches[sectorId]
		if ok {
			sectorFileCacheLk.Unlock()
			result[sectorId] = sFile.SectorFile
			continue
		}
		sectorFileCacheLk.Unlock()

		file := &storiface.SectorFile{
			SectorId:     sectorId,
			SealedRepo:   defaultRepo,
			UnsealedRepo: defaultRepo,
		}

		storageSealed := int64(0)
		storageUnsealed := int64(0)
		if err := sectorStmt.
			QueryRow(sectorId).
			Scan(&storageSealed, &storageUnsealed); err != nil {
			if err != sql.ErrNoRows {
				return nil, errors.As(err, sectorId)
			}
			// sector not found in db, return default.
		} else {
			// fix the file
			sealedPoint, ok := storages[storageSealed]
			if ok {
				file.SealedRepo = filepath.Join(sealedPoint.MountDir, fmt.Sprintf("%d", storageSealed))
				file.SealedStorageId = storageSealed
				file.SealedStorageType = sealedPoint.MountType
				if sealedPoint.MountType == MOUNT_TYPE_OSS {
					file.SealedRepo = filepath.Join(sealedPoint.MountDir, fmt.Sprintf("%s", sectorId))
				}
			}
			unsealedPoint, ok := storages[storageUnsealed]
			if ok {
				file.UnsealedRepo = filepath.Join(unsealedPoint.MountDir, fmt.Sprintf("%d", storageUnsealed))
				file.UnsealedStorageId = storageUnsealed
				file.UnsealedStorageType = unsealedPoint.MountType
				file.IsMarketSector = true
				if sealedPoint.MountType == MOUNT_TYPE_OSS {
					file.UnsealedRepo = filepath.Join(unsealedPoint.MountDir, fmt.Sprintf("%s", sectorId))
				}
			}
		}
		result[sectorId] = *file

		// put to cache
		sectorFileCacheLk.Lock()
		sectorFileCaches[sectorId] = sectorFileCache{
			SectorFile: *file,
			CreateTime: time.Now(),
		}
		sectorFileCacheLk.Unlock()
	}
	return result, nil
}

func GetSectorFile(sectorId, defaultRepo string) (*storiface.SectorFile, error) {
	startTime := time.Now()
	defer func() {
		took := time.Now().Sub(startTime)
		if took > 5e8 {
			log.Warnf("GetSectorFile(%s) took : %s", sectorId, took)
		}
	}()

	file := &storiface.SectorFile{
		SectorId:     sectorId,
		SealedRepo:   defaultRepo,
		UnsealedRepo: defaultRepo,
	}
	if !HasDB() {
		return file, nil
	}
	// read from cache
	sectorFileCacheLk.Lock()
	sFile, ok := sectorFileCaches[sectorId]
	if ok {
		sectorFileCacheLk.Unlock()
		return &sFile.SectorFile, nil
	}
	sectorFileCacheLk.Unlock()

	mdb := GetDB()

	storageSealed := int64(0)
	storageUnsealed := int64(0)
	if err := mdb.
		QueryRow("SELECT storage_sealed,storage_unsealed FROM sector_info WHERE id=?", sectorId).
		Scan(&storageSealed, &storageUnsealed); err != nil {
		if err != sql.ErrNoRows {
			return nil, errors.As(err, sectorId)
		}
		// sector not found in db, return default.
		return file, nil
	}

	var sealedPoint *StorageMountPoint
	var unsealedPoint *StorageMountPoint
	rows, err := mdb.Query(fmt.Sprintf("SELECT id, mount_dir,mount_type FROM storage_info WHERE id IN (%d,%d)", storageSealed, storageUnsealed))
	if err != nil {
		return nil, errors.As(err, sectorId)
	}
	defer rows.Close()
	for rows.Next() {
		id := int64(0)
		mountPoint := StorageMountPoint{}
		if err := rows.Scan(&id, &mountPoint.MountDir, &mountPoint.MountType); err != nil {
			return nil, errors.As(err, sectorId)
		}
		if id == storageSealed {
			sealedPoint = &mountPoint
		}
		if id == storageUnsealed {
			unsealedPoint = &mountPoint
		}
	}
	if sealedPoint != nil {
		if sealedPoint.MountType == "oss" {
			file.SealedRepo = sealedPoint.MountDir
		} else {
			file.SealedRepo = filepath.Join(sealedPoint.MountDir, fmt.Sprintf("%d", storageSealed))
		}
		file.SealedStorageId = storageSealed
		file.SealedStorageType = sealedPoint.MountType
	}
	if unsealedPoint != nil {
		if unsealedPoint.MountType == "oss" {
			file.UnsealedRepo = unsealedPoint.MountDir
		} else {
			file.UnsealedRepo = filepath.Join(unsealedPoint.MountDir, fmt.Sprintf("%d", storageUnsealed))
		}
		file.UnsealedStorageId = storageUnsealed
		file.UnsealedStorageType = unsealedPoint.MountType
		file.IsMarketSector = true
	}

	// put to cache
	sectorFileCacheLk.Lock()
	sectorFileCaches[sectorId] = sectorFileCache{
		SectorFile: *file,
		CreateTime: time.Now(),
	}
	sectorFileCacheLk.Unlock()

	return file, nil
}

type SectorList []SectorInfo

func GetSectorByState(storageSealed int64, state int64) (SectorList, error) {
	mdb := GetDB()
	list := SectorList{}
	if err := database.QueryStructs(mdb, &list, "SELECT * FROM sector_info WHERE storage_sealed=? AND state=?", storageSealed, state); err != nil {
		return nil, errors.As(err, storageSealed)
	}
	return list, nil
}

func GetAllSectorByState(state int64) (map[string]int64, error) {
	mdb := GetDB()
	rows, err := mdb.Query("SELECT id,storage_sealed FROM sector_info WHERE state=?", state)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		sid := ""
		storageSealed := int64(0)
		if err := rows.Scan(&sid, &storageSealed); err != nil {
			return nil, errors.As(err)
		}
		result[sid] = storageSealed
	}
	if len(result) == 0 {
		return nil, errors.ErrNoData.As(state)
	}
	return result, nil
}

func GetSectorStorage(id string) (*SectorStorage, error) {
	mdb := GetDB()
	// TODO: make left join
	seInfo := &SectorInfo{
		ID: id,
	}
	if err := database.QueryStruct(mdb, seInfo, "SELECT * FROM sector_info WHERE id=?", id); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		seInfo.WorkerId = "default"
		seInfo.StorageSealed = 1
	}
	wkInfo := &WorkerInfo{
		ID: seInfo.WorkerId,
	}
	if err := database.QueryStruct(mdb, wkInfo, "SELECT * FROM worker_info WHERE id=?", seInfo.WorkerId); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}

		// upgrade fixed for worker ip
	}
	sealedInfo := &StorageInfo{}
	if err := database.QueryStruct(mdb, sealedInfo, "SELECT * FROM storage_info WHERE id=?", seInfo.StorageSealed); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}
	}
	unsealedInfo := &StorageInfo{}
	if err := database.QueryStruct(mdb, unsealedInfo, "SELECT * FROM storage_info WHERE id=?", seInfo.StorageUnsealed); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}
	}
	return &SectorStorage{
		SectorInfo:      *seInfo,
		WorkerInfo:      *wkInfo,
		SealedStorage:   *sealedInfo,
		UnsealedStorage: *unsealedInfo,
	}, nil
}

func GetSectorSnapValue(snap bool) int {
	if snap {
		return 1
	}
	return 0
}


func UpdateSectorAbortSnapState(sid string) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	state=200,
    snap=0
WHERE
	id=?
	
`, sid); err != nil {
		return errors.As(err)
	}
	return nil
}

func UpdateSectorState(sid, wid, msg string, state, snap int) error {

	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	worker_id=?,
	state=?,
    snap=?,
	state_time=?,
	state_msg=?,
	state_times=state_times+1
WHERE
	id=?
	
`, wid, state, snap, time.Now(), msg, sid); err != nil {
		return errors.As(err)
	}
	return nil
}

type WorkingSectors []SectorInfo

func (ws WorkingSectors) IsFullWork(maxTaskNum, cacheNum int) bool {
	working := []*SectorInfo{}
	pushing := []*SectorInfo{}
	for _, w := range ws {
		if w.State < SECTOR_STATE_PUSH {
			working = append(working, &w)
		} else if w.State < SECTOR_STATE_DONE {
			pushing = append(pushing, &w)
		}
	}
	cacheCap := len(pushing) - cacheNum
	if cacheCap < 0 {
		cacheCap = 0
	}
	if cacheCap+len(working) >= maxTaskNum {
		return true
	}
	return false
}
func GetWorking(workerId string) (WorkingSectors, error) {
	mdb := GetDB()
	sectors := WorkingSectors{}
	if err := database.QueryStructs(mdb, &sectors, "SELECT * FROM sector_info WHERE worker_id=? AND state<200", workerId); err != nil {
		return nil, errors.As(err, workerId)
	}
	return sectors, nil
}

// Only called in  cache-mode=1
// SPECS: not call in more than 1000 tasks.
func CheckWorkingById(sid []string) (WorkingSectors, error) {
	startTime := time.Now()
	defer func() {
		took := time.Now().Sub(startTime)
		if took > 5e9 {
			log.Warnf("CheckWorkingById len(%d) took : %s", len(sid), took)
		}
	}()

	sectors := WorkingSectors{}
	if len(sid) == 0 {
		return sectors, nil
	}
	args := []rune{}
	for _, s := range sid {
		// checking sql injection
		if _, err := storiface.ParseSectorID(s); err != nil {
			return sectors, errors.As(err, sid)
		}
		args = append(args, []rune(",'")...)
		args = append(args, []rune(s)...)
		args = append(args, []rune("'")...)
	}
	if len(args) > 0 {
		args = args[1:] // remove ',' in the head.
	}

	mdb := GetDB()
	sqlStr := fmt.Sprintf("SELECT * FROM sector_info WHERE id in (%s) AND state<200", string(args))
	if err := database.QueryStructs(mdb, &sectors, sqlStr); err != nil {
		return nil, errors.As(err)
	}
	return sectors, nil
}

func RebuildSector(sid string, storage uint64) error {
	mdb := GetDB()
	result, err := mdb.Exec("UPDATE sector_rebuild SET storage_sealed=? WHERE id=?", storage, sid)
	if err != nil {
		return errors.As(err, sid, storage)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.As(err, sid, storage)
	}
	if rows == 0 {
		if _, err := mdb.Exec("INSERT INTO sector_rebuild(id,storage_sealed)VALUES(?,?)", sid, storage); err != nil {
			return errors.As(err, sid, storage)
		}
	}
	return nil
}
func RebuildSectorProcess(sid string) (int, uint64, error) {
	mdb := GetDB()
	state := -1
	storage := uint64(0)
	if err := mdb.QueryRow("SELECT tb1.state, tb2.storage_sealed FROM sector_info tb1 INNER JOIN sector_rebuild tb2 ON tb1.id=tb2.id WHERE tb1.id=?", sid).Scan(&state, &storage); err != nil {
		if sql.ErrNoRows == err {
			return state, storage, errors.ErrNoData.As(sid)
		}
		return state, storage, errors.As(err, sid)
	}
	return state, storage, nil
}

func RebuildSectorDone(sid string) error {
	mdb := GetDB()
	if _, err := mdb.Exec("DELETE FROM sector_rebuild WHERE id=?", sid); err != nil {
		return errors.As(err)
	}
	return nil
}

func SetSectorSealedStorage(sid string, storage uint64) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE sector_info SET storage_sealed=? WHERE id=?", storage, sid); err != nil {
		return errors.As(err, sid, storage)
	}
	sectorFileCacheLk.Lock()
	delete(sectorFileCaches, sid)
	sectorFileCacheLk.Unlock()
	return nil
}
func SetSectorUnSealedStorage(sid string, storage uint64) error {
	mdb := GetDB()
	if _, err := mdb.Exec("UPDATE sector_info SET storage_unsealed=? WHERE id=?", storage, sid); err != nil {
		return errors.As(err, sid, storage)
	}
	sectorFileCacheLk.Lock()
	delete(sectorFileCaches, sid)
	sectorFileCacheLk.Unlock()
	return nil
}

func ResetSectorWorkerID(sid string) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	worker_id="",
	state_time=?,
	state_msg=?,
	state_times=state_times+1
WHERE
	id=?
	
`,  time.Now(), "snap", sid); err != nil {
		return errors.As(err)
	}
	return nil
}