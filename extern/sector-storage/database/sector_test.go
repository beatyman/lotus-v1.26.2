package database

import (
	"fmt"
	"testing"
	"time"
)

func TestSectorFile(t *testing.T) {
	InitDB("/data/sdb/lotus-user-1/.lotusstorage/")
	_, err := GetSectorFile("s-t010313-1", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := GetSectorsFile([]string{"s-t010313-1","s-t010313-2"}, "")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(result)
}

func TestSectorInfo(t *testing.T) {
	InitDB("./")
	minerId := "t0101"
	sectorId := time.Now().UnixNano()
	id := fmt.Sprintf("s-%s-%d", minerId, sectorId)
	info := &SectorInfo{
		ID:            id,
		MinerId:       minerId,
		UpdateTime:    time.Now(),
		StorageSealed: 1,
		WorkerId:      "default",
		CreateTime:    time.Now(),
	}
	if err := AddSectorInfo(info); err != nil {
		t.Fatal(err)
	}

	exp, err := GetSectorInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if exp.WorkerId != info.WorkerId || info.StorageSealed != exp.StorageSealed {
		t.Fatal(*info, *exp)
	}
}

func TestCheckWorkingById(t *testing.T) {
	InitDB("/data/sdb/lotus-user-1/.lotusstorage")
	//working, err := CheckWorkingById([]string{"s-t080868-0","s-t080868-100", "s-t080868-1000", "s-t080868-10000"})
	working, err := CheckWorkingById([]string{})
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("%+v\n", working)
}
