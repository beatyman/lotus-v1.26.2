package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/gwaylib/errors"
)

func TestDiskUsage(t *testing.T) {
	diskStatus, err := DiskUsage("/")
	if err != nil {
		t.Fatal(err)
	} else {
		fmt.Println("size:", diskStatus.All)
	}
}

func TestAllocateStorage(t *testing.T) {
	InitDB("./")
	db, lk := GetDB()
	defer lk.Unlock()

	// clean test case
	if _, err := db.Exec("DELETE FROM storage_info"); err != nil {
		t.Fatal(err)
	}
	info := &StorageInfo{
		UpdateTime:     time.Now(),
		MaxSize:        10240, // 10 sector size
		KeepSize:       1024,  // keep one sector
		SectorSize:     1024,
		MaxWork:        5,
		MountSignalUri: "127.0.0.1:/data/zfs",
		MountTransfUri: "127.0.0.1:/data/zfs",
	}
	if _, err := AddStorageInfo(info); err != nil {
		t.Fatal(err)
	}

	// case 0 make a error cancel
	tx, aInfo, err := PrepareStorage("0", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// case 1, return one valid storage at least.
	for i := 0; i < 9; i++ {
		tx, aInfo, err = PrepareStorage("0", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		cInfo, err := GetStorageInfo(aInfo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if aInfo.CurWork != cInfo.CurWork {
			t.Fatal(*aInfo, *cInfo)
		}
		if aInfo.UsedSize != cInfo.UsedSize-cInfo.SectorSize {
			t.Fatal(*aInfo, *cInfo)
		}
	}

	// case 2, testing full storage , it expect no sector allcate.
	if _, _, err := PrepareStorage("0", "", 0); !errors.ErrNoData.Equal(err) {
		t.Fatal(err)
	}
}

func TestMountAllStorage(t *testing.T) {
	InitDB("./")
	db, lk := GetDB()
	defer lk.Unlock()

	// analogue data
	if _, err := db.Exec("DELETE FROM storage_info"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM sqlite_sequence WHERE name='storage_info'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE sqlite_sequence SET seq=0 WHERE name='storage_info'"); err != nil {
		t.Fatal(err)
	}

	// for local
	if _, err := AddStorageInfo(&StorageInfo{
		UpdateTime:     time.Now(),
		MaxSize:        922372036854775807,
		SectorSize:     107374182400,
		MaxWork:        1000,
		MountSignalUri: "/data/sdb/lotus-user-1",
		MountTransfUri: "/data/sdb/lotus-user-1",
		MountDir:       "/data/testing",
	}); err != nil {
		t.Fatal(err)
	}

	// for net work, make sure it exists.
	if _, err := AddStorageInfo(&StorageInfo{
		UpdateTime:     time.Now(),
		MaxSize:        922372036854775807,
		SectorSize:     107374182400,
		MaxWork:        1000,
		MountType:      "nfs",
		MountSignalUri: "127.0.0.1:/data/zfs",
		MountTransfUri: "127.0.0.1:/data/zfs",
		MountDir:       "/data/testing",
	}); err != nil {
		t.Fatal(err)
	}
	// for net work, make sure it exists.
	if _, err := AddStorageInfo(&StorageInfo{
		UpdateTime:     time.Now(),
		MaxSize:        922372036854775807,
		SectorSize:     107374182400,
		MaxWork:        1000,
		MountType:      "nfs",
		MountSignalUri: "127.0.0.1:/data/zfs",
		MountTransfUri: "127.0.0.1:/data/zfs",
		MountDir:       "/data/testing",
		MountOpt:       "-o noatime,nodev,nosuid",
	}); err != nil {
		t.Fatal(err)
	}

	if err := MountAllStorage(false); err != nil {
		t.Fatal(err)
	}
	// checksum the result by manu.
	// it should have a link file with /data/testing/1, and mount point with /data/testing/2
}
