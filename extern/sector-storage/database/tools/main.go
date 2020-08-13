package main

import (
	"fmt"

	"github.com/filecoin-project/sector-storage/database"
	dbtool "github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

func main() {
	database.InitDB("/data/sdb/lotus-user-1/.lotusstorage")
	db := database.GetDB()

	rebalanceCurWork(db)

	fmt.Println("done")
}

// rebalance work requirement:
// 重平衡每台存储节点上的工作数, 以下是前置条件:
// 1, the task could not at commit stage. the commit stage is the last get WorkerCfg.
// 1, 计算任务不能是commit阶段，commit阶段是WorkerCfg最后一次被获取的地方。
func rebalanceCurWork(db *dbtool.DB) {
	storages := []database.StorageInfo{}
	if err := dbtool.QueryStructs(db, &storages, "SELECT * FROM storage_info ORDER BY ID"); err != nil {
		panic(errors.As(err))
	}
	sectors := []database.SectorInfo{}
	if err := dbtool.QueryStructs(db, &sectors, "SELECT * FROM sector_info WHERE state<1"); err != nil {
		panic(errors.As(err))
	}
	// fmt.Printf("%+v\n", storages)
	// fmt.Printf("%+v\n", sectors)

	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	for {
		var fromStorage *database.StorageInfo
		fromWorkLevel := 10
		slashWork := 0
		for i, st := range storages {
			if st.CurWork > fromWorkLevel {
				fromStorage = &storages[i]
				slashWork = st.CurWork - fromWorkLevel
				break
			}
		}
		if fromStorage == nil {
			fmt.Println("No storage need transfer")
			break
		}

		var toStorage *database.StorageInfo
		toWorkLevel := 3 // reblance to work
		for i, st := range storages {
			if st.CurWork < toWorkLevel {
				toStorage = &storages[i]
				break
			}
		}
		if toStorage == nil {
			fmt.Println("No storage to transfer", fromStorage.ID)
			break
		}

		for i := 0; i < slashWork; i++ {
			for j := len(sectors) - 1; j > -1; j-- {
				se := &sectors[j]
				if se.StorageId == fromStorage.ID && toStorage.CurWork < toWorkLevel {
					if _, err := tx.Exec("UPDATE sector_info SET storage_id=? WHERE id=?", toStorage.ID, se.ID); err != nil {
						dbtool.Rollback(tx)
						panic(err)
					}
					if _, err := tx.Exec("UPDATE storage_info SET cur_work=cur_work-1 WHERE id=?", fromStorage.ID); err != nil {
						dbtool.Rollback(tx)
						panic(err)
					}
					if _, err := tx.Exec("UPDATE storage_info SET cur_work=cur_work+1 WHERE id=?", toStorage.ID); err != nil {
						dbtool.Rollback(tx)
						panic(err)
					}
					se.StorageId = toStorage.ID
					fromStorage.CurWork -= 1
					toStorage.CurWork += 1
					break // found and dealed one, find the next one.
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		dbtool.Rollback(tx)
		panic(err)
	}
	db.Close()
}
