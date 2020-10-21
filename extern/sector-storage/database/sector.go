package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
	"golang.org/x/xerrors"
)

const (
	SECTOR_STATE_INIT = 0

	SECTOR_STATE_MOVE = 100
	SECTOR_STATE_PUSH = 101

	SECTOR_STATE_DONE   = 200
	SECTOR_STATE_FAILED = 500
)

func ParseSectorID(baseName string) (abi.SectorID, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
	if err != nil {
		return abi.SectorID{}, xerrors.Errorf("sscanf sector name ('%s'): %w", baseName, err)
	}

	if read != 2 {
		return abi.SectorID{}, xerrors.Errorf("parseSectorID expected to scan 2 values, got %d", read)
	}

	return abi.SectorID{
		Miner:  mid,
		Number: n,
	}, nil
}

func SectorName(sid abi.SectorID) string {
	return fmt.Sprintf("s-t0%d-%d", sid.Miner, sid.Number)
}

type SectorFile struct {
	SectorId  string
	StorageId uint64
}

func (f *SectorFile) SectorID() abi.SectorID {
	id, err := ParseSectorID(f.SectorId)
	if err != nil {
		// should not reach here.
		panic(err)
	}
	return id
}

func (f *SectorFile) UnsealedFile() string {
	return fmt.Sprintf("/data/nfs/%d/unsealed/%s", f.StorageId, f.SectorId)
}
func (f *SectorFile) SealedFile() string {
	return fmt.Sprintf("/data/nfs/%d/sealed/%s", f.StorageId, f.SectorId)
}
func (f *SectorFile) CachePath() string {
	return fmt.Sprintf("/data/nfs/%d/cache/%s", f.StorageId, f.SectorId)
}

type SectorInfo struct {
	ID         string    `db:"id"` // s-t0101-1
	MinerId    string    `db:"miner_id"`
	UpdateTime time.Time `db:"updated_at"`
	StorageId  int64     `db:"storage_id"`
	WorkerId   string    `db:"worker_id"`
	State      int       `db:"state,0"`
	StateTime  time.Time `db:"state_time"`
	StateTimes int       `db:"state_times"`
	CreateTime time.Time `db:"created_at"`
}

type SectorStorage struct {
	SectorInfo  SectorInfo
	StorageInfo StorageInfo
	WorkerInfo  WorkerInfo
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
		info.StorageId = 1
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

func GetSectorFile(id string) (*SectorFile, error) {
	file := SectorFile{}
	mdb := GetDB()
	if err := mdb.QueryRow("SELECT storage_id FROM sector_info WHERE id=?", id).Scan(
		&file.StorageId,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.ErrNoData.As(id)
		}
		return nil, errors.As(err, id)
	}
	file.SectorId = id
	return &file, nil
}

type SectorList []SectorInfo

func GetSectorByState(storageId int64, state int64) (SectorList, error) {
	mdb := GetDB()
	list := SectorList{}
	if err := database.QueryStructs(mdb, &list, "SELECT * FROM sector_info WHERE storage_id=? AND state=?", storageId, state); err != nil {
		return nil, errors.As(err, storageId)
	}
	return list, nil
}

func GetAllSectorByState(state int64) (map[string]int64, error) {
	mdb := GetDB()
	rows, err := mdb.Query("SELECT id,storage_id FROM sector_info WHERE state=?", state)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		sid := ""
		storageId := int64(0)
		if err := rows.Scan(&sid, &storageId); err != nil {
			return nil, errors.As(err)
		}
		result[sid] = storageId
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
		seInfo.StorageId = 1
	}
	stInfo := &StorageInfo{}
	if err := database.QueryStruct(mdb, stInfo, "SELECT * FROM storage_info WHERE id=?", seInfo.StorageId); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}
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
	return &SectorStorage{
		SectorInfo:  *seInfo,
		StorageInfo: *stInfo,
		WorkerInfo:  *wkInfo,
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
		if _, err := ParseSectorID(s); err != nil {
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
