package database

import (
	"fmt"
	"testing"
)

func TestSearchStorageInfo(t *testing.T) {
	InitDB("/data/sdb/lotus-user-1/.lotusstorage")

	data, err := SearchStorageInfoBySignalIp("10.1.1.3")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(data)
}
