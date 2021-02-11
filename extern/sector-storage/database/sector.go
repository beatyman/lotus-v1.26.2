package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/specs-storage/storage"
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

type StorageStatus struct {
	StorageId int64
	MountDir  string
	MountUri  string
	Disable   bool
	Used      time.Duration
	Err       string
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
	if err := database.QueryStruct(mdb, info, "SELECT * FROM sector_info WHERE id=?", id); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		info.StorageSealed = 1
		info.WorkerId = "default"
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

func FillSectorFile(sector storage.SectorRef, defaultRepo string) (storage.SectorRef, error) {
	if sector.HasRepo() {
		return sector, nil
	}
	// set to default.
	sector.SectorId = storage.SectorName(sector.ID)
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
	SectorFile storage.SectorFile
	CreateTime time.Time
}

var (
	sectorFileCaches  = map[string]sectorFileCache{}
	sectorFileCacheLk = sync.Mutex{}
)

func gcSectorFileCache() {
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

func GetSectorsFile(sectors []string, defaultRepo string) (map[string]storage.SectorFile, error) {
	log.Infof("GetSectorsFile in, len:%d", len(sectors))
	startTime := time.Now()
	defer func() {
		took := time.Now().Sub(startTime)
		if took > 5e9 {
			log.Warnf("GetSectorsFile took : %s", took)
		}
		log.Infof("GetSectorsFile out, len:%d", len(sectors))
	}()

	defaultResult := map[string]storage.SectorFile{}
	if !HasDB() {
		for _, sectorId := range sectors {
			defaultResult[sectorId] = storage.SectorFile{
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
	storages := map[uint64]sql.NullString{} // id:mount_dir
	rows, err := mdb.Query("SELECT id, mount_dir FROM storage_info")
	if err != nil {
		return nil, errors.As(err, sectors)
	}
	for rows.Next() {
		id := uint64(0)
		dir := sql.NullString{}
		if err := rows.Scan(&id, &dir); err != nil {
			database.Close(rows)
			return nil, errors.As(err, sectors)
		}
		storages[id] = dir
	}
	database.Close(rows)
	log.Info("DEBUG:storages loaded")

	// get sector info
	sectorStmt, err := mdb.Prepare("SELECT storage_sealed,storage_unsealed FROM sector_info WHERE id=?")
	if err != nil {
		return nil, errors.As(err, sectors)
	}
	defer sectorStmt.Close()

	log.Info("DEBUG:sectors preparing")
	result := map[string]storage.SectorFile{}
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

		file := &storage.SectorFile{
			SectorId:     sectorId,
			SealedRepo:   defaultRepo,
			UnsealedRepo: defaultRepo,
		}

		storageSealed := uint64(0)
		storageUnsealed := uint64(0)
		if err := sectorStmt.
			QueryRow(sectorId).
			Scan(&storageSealed, &storageUnsealed); err != nil {
			if err != sql.ErrNoRows {
				return nil, errors.As(err, sectorId)
			}
			// sector not found in db, return default.
		} else {
			// fix the file
			storageSealedDir, _ := storages[storageSealed]
			storageUnsealedDir, _ := storages[storageUnsealed]
			if storageSealedDir.Valid {
				file.SealedRepo = filepath.Join(storageSealedDir.String, fmt.Sprintf("%d", storageSealed))
			}
			if storageUnsealedDir.Valid {
				file.UnsealedRepo = filepath.Join(storageUnsealedDir.String, fmt.Sprintf("%d", storageUnsealed))
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

func GetSectorFile(sectorId, defaultRepo string) (*storage.SectorFile, error) {
	startTime := time.Now()
	defer func() {
		took := time.Now().Sub(startTime)
		if took > 5e8 {
			log.Warnf("GetSectorFile(%s) took : %s", sectorId, took)
		}
	}()

	file := &storage.SectorFile{
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

	storageSealed := uint64(0)
	storageUnsealed := uint64(0)
	if err := mdb.
		QueryRow("SELECT storage_sealed,storage_unsealed FROM sector_info WHERE id=?", sectorId).
		Scan(&storageSealed, &storageUnsealed); err != nil {
		if err != sql.ErrNoRows {
			return nil, errors.As(err, sectorId)
		}
		// sector not found in db, return default.
		return file, nil
	}

	storageSealedDir := sql.NullString{}
	storageUnsealedDir := sql.NullString{}
	rows, err := mdb.Query(fmt.Sprintf("SELECT id, mount_dir FROM storage_info WHERE id IN (%d,%d)", storageSealed, storageUnsealed))
	if err != nil {
		return nil, errors.As(err, sectorId)
	}
	defer rows.Close()
	for rows.Next() {
		id := uint64(0)
		dir := sql.NullString{}
		if err := rows.Scan(&id, &dir); err != nil {
			return nil, errors.As(err, sectorId)
		}
		if id == storageSealed {
			storageSealedDir = dir
		}
		if id == storageUnsealed {
			storageUnsealedDir = dir
		}
	}

	if storageSealedDir.Valid {
		file.SealedRepo = filepath.Join(storageSealedDir.String, fmt.Sprintf("%d", storageSealed))
	}
	if storageUnsealedDir.Valid {
		file.UnsealedRepo = filepath.Join(storageUnsealedDir.String, fmt.Sprintf("%d", storageUnsealed))
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

func UpdateSectorState(sid, wid, msg string, state int) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	worker_id=?,
	state=?,
	state_time=?,
	state_msg=?,
	state_times=state_times+1
WHERE
	id=?
	
`, wid, state, time.Now(), msg, sid); err != nil {
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
	sectors := WorkingSectors{}
	args := []rune{}
	for _, s := range sid {
		// checking sql injection
		if _, err := storage.ParseSectorID(s); err != nil {
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
