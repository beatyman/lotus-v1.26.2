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

func TestAddStorage(t *testing.T) {
	InitDB("./")
	storageInfo := &StorageInfo{
		MaxSize:        922372036854775807,
		MaxWork:        1000,
		MountType:      "nfs",
		MountSignalUri: "127.0.0.1:/data/zfs",
		MountTransfUri: "127.0.0.1:/data/zfs",
		MountDir:       "/data/nfs",
		MountOpt:       "-o proto=tcp -o nolock -o port=2049",
	}
	if _, err := AddStorageInfo(storageInfo); err != nil {
		t.Fatal(err)
	}
}
