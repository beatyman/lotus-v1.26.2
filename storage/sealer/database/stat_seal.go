package database

import (
	"fmt"
	"sort"
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

const (
	SEAL_STAGE_AP         = "addpiece"
	SEAL_STAGE_US         = "unsealed"
	SEAL_STAGE_P1         = "precommit1"
	SEAL_STAGE_P2         = "precommit2"
	SEAL_STAGE_UNLOCK_SRV = "unlock_gpu_srv" // c2, or wdpost, or wnpost
	SEAL_STAGE_COMMIT     = "commit"
	SEAL_STAGE_C2         = "commit2"
	SEAL_STAGE_R2         = "replica2"
	SEAL_STAGE_FZ         = "finalize"
	SEAL_STAGE_WD         = "wdpost"
	SEAL_STAGE_WN         = "wnpost"
)

type StatisSeal struct {
	TaskID    string    `db:"task_id"`
	Sid       string    `db:"sid"`
	Stage     string    `db:"stage"`
	WorkerID  string    `db:"worker_id"`
	BeginTime time.Time `db:"begin_time"`
	EndTime   time.Time `db:"end_time"`
	Used      int64     `db:"used"`
	Error     string    `db:"error"` // success, other's failed
	IsRsync   int64     `db:"is_rsync"`
}

func PutStatisSeal(st *StatisSeal) error {
	if st == nil {
		return nil
	}
	db := GetDB()
	tbName, err := createMonthTable(db, tb_statis_seal_headname, tb_statis_seal_sql, st.BeginTime)
	if err != nil {
		return errors.As(err, *st)
	}

	existID := ""
	if err := database.QueryElem(db, &existID,
		fmt.Sprintf("SELECT task_id FROM %s WHERE task_id=? AND sid=?", tbName),
		st.TaskID, st.Sid); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return errors.As(err, *st)
		}
	}

	if len(existID) == 0 {
		if _, err := database.InsertStruct(db, st, tbName); err != nil {
			return errors.As(err, *st)
		}
		return nil
	}

	// update
	if _, err := db.Exec(fmt.Sprintf(`
UPDATE %s
SET
  updated_at=?,
  stage=?,
  worker_id=?,
  begin_time=?,
  end_time=?,
  used=?,
  error=?
WHERE
  task_id=?
  AND sid=?
`, tbName),
		time.Now(),
		st.Stage,
		st.WorkerID,
		st.BeginTime, st.EndTime,
		st.Used, st.Error,
		st.TaskID, st.Sid,
	); err != nil {
		return errors.As(err, *st)
	}
	return nil
}

const (
	statWorkerSealNumSql = `
SELECT 
	worker_id,stage,count(*) num
FROM %s
WHERE
	created_at between ? and ? 
	and error='success'
GROUP BY worker_id, stage
`
)

type StatWorkerSealNum struct {
	WorkerID string
	APNum    int
	USNum    int
	P1Num    int
	P2Num    int
	C2Num    int
	FZNum    int
}

type StatWorkerSealNums []StatWorkerSealNum

func (s StatWorkerSealNums) SortByID() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].WorkerID < s[j].WorkerID
	})
}
func (s StatWorkerSealNums) SortByAddPiece() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].APNum < s[j].APNum
	})
}
func (s StatWorkerSealNums) SortByUnseal() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].USNum < s[j].USNum
	})
}
func (s StatWorkerSealNums) SortByPrecommit1() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].P1Num < s[j].P1Num
	})
}
func (s StatWorkerSealNums) SortByPrecommit2() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].P2Num < s[j].P2Num
	})
}
func (s StatWorkerSealNums) SortByCommit2() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].C2Num < s[j].C2Num
	})
}
func (s StatWorkerSealNums) SortByFinalized() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].FZNum < s[j].FZNum
	})
}
func (s StatWorkerSealNums) Dump() {
	fmt.Println("====================")
	for _, r := range s {
		fmt.Printf("WorkerID:%s, AP:%d, US:%d, P1:%d, P2:%d, C2:%d, FZ:%d\n",
			r.WorkerID, r.APNum, r.USNum, r.P1Num, r.P2Num, r.C2Num, r.FZNum,
		)
	}
}
func (s StatWorkerSealNums) SumDump() {
	var apNum, usNum, p1Num, p2Num, c2Num, fzNum int
	for _, r := range s {
		apNum += r.APNum
		usNum += r.USNum
		p1Num += r.P1Num
		p2Num += r.P2Num
		c2Num += r.C2Num
		fzNum += r.FZNum
	}
	fmt.Printf("SUM, AP:%d, US:%d, P1:%d, P2:%d, C2:%d, FZ:%d\n", apNum, usNum, p1Num, p2Num, c2Num, fzNum)
}

func StatWorkerSealNumFn(startTime, endTime time.Time) (StatWorkerSealNums, error) {
	tbName := getMonthTableName(tb_statis_seal_headname, endTime)
	destSql := fmt.Sprintf(statWorkerSealNumSql, tbName)
	db := GetDB()
	rows, err := db.Query(destSql, startTime, endTime)
	if err != nil {
		return nil, errors.As(err)
	}
	defer database.Close(rows)

	result := map[string]StatWorkerSealNum{}
	for rows.Next() {
		workerID := ""
		stage := ""
		num := 0
		if err := rows.Scan(&workerID, &stage, &num); err != nil {
			return nil, errors.As(err)
		}
		st, ok := result[workerID]
		if !ok {
			st.WorkerID = workerID
		}
		switch stage {
		case SEAL_STAGE_AP:
			st.APNum += num
		case SEAL_STAGE_US:
			st.USNum += num
		case SEAL_STAGE_P1:
			st.P1Num += num
		case SEAL_STAGE_P2:
			st.P2Num += num
		case SEAL_STAGE_COMMIT, SEAL_STAGE_C2:
			st.C2Num += num
		case SEAL_STAGE_FZ:
			st.FZNum += num
		}
		result[workerID] = st
	}

	arrResult := make([]StatWorkerSealNum, len(result))
	idx := 0
	for _, w := range result {
		arrResult[idx] = w
		idx++
	}
	return arrResult, nil
}

const (
	statWorkerSealTimeSql = `
SELECT
	stage, count(*) num, cast(avg(used) as integer) avg, min(used) min, max(used) max 
FROM
%s
WHERE
	worker_id=? 
	and created_at between ? and ? 
	and error='success'
GROUP BY stage;
`
	sectorByWorkerSql = `
SELECT
	*
FROM
%s
WHERE
	sid = ? 
union all 
SELECT
	*
FROM
%s
WHERE
	sid = ? 
;
`
)

type StatWorkerSealTime struct {
	Stage string
	Total int
	Avg   int64
	Min   int64
	Max   int64
}

type StatWorkerSealTimes []StatWorkerSealTime

func (s StatWorkerSealTimes) Dump() {
	fmt.Println("====================")
	for _, r := range s {
		fmt.Printf("Stage:%s, total:%d, avg:%ds, min:%ds, max:%ds\n",
			r.Stage, r.Total, r.Avg, r.Min, r.Max)
	}
}

func StatWorkerSealTimeFn(workerID string, startTime, endTime time.Time) (StatWorkerSealTimes, error) {
	tbName := getMonthTableName(tb_statis_seal_headname, endTime)
	destSql := fmt.Sprintf(statWorkerSealTimeSql, tbName)
	db := GetDB()
	rows, err := db.Query(destSql, workerID, startTime, endTime)
	if err != nil {
		return nil, errors.As(err, endTime)
	}
	defer database.Close(rows)

	result := StatWorkerSealTimes{}
	for rows.Next() {
		stat := StatWorkerSealTime{}
		if err := rows.Scan(&stat.Stage, &stat.Total, &stat.Avg, &stat.Min, &stat.Max); err != nil {
			return nil, errors.As(err)
		}
		result = append(result, stat)
	}
	return result, nil
}

func GetSectorByWorker(sectorID, lastMonth, currentMonth string) ([]StatWorkerSealTime, error) {
	destSql := fmt.Sprintf(sectorByWorkerSql, lastMonth, currentMonth)
	db := GetDB()
	rows, err := db.Query(destSql, sectorID)
	if err != nil {
		return nil, errors.As(err, sectorID)
	}
	defer database.Close(rows)

	result := StatWorkerSealTimes{}
	for rows.Next() {
		stat := StatWorkerSealTime{}
		if err := rows.Scan(&stat.Stage, &stat.Total, &stat.Avg, &stat.Min, &stat.Max); err != nil {
			return nil, errors.As(err)
		}
		result = append(result, stat)
	}
	return result, nil
}
