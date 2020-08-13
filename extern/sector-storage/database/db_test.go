package database

import (
	"testing"
)

func TestSqlite(t *testing.T) {
	InitDB("./")
	db := GetDB()
	num := 0
	if err := db.QueryRow("SELECT COUNT(*) FROM sector_info").Scan(&num); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO storage_info(max_size,mount_signal_uri)VALUES(?,?)", 34359738368*30*8, "127.0.0.1:/data/zfs"); err != nil {
		t.Fatal(err)
	}
}

func TestSqliteSingleInsert(t *testing.T) {
	InitDB("./")
	db := GetDB()
	for i := 0; i < 1000; i++ {
		if _, err := db.Exec("INSERT INTO storage_info(max_size,mount_siganl_uri)VALUES(?,?)", 34359738368*30*8, "127.0.0.1:/data/zfs"); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSqliteSingleQuery(t *testing.T) {
	InitDB("./")
	db := GetDB()
	for i := 0; i < 1000; i++ {
		uri := ""
		if err := db.QueryRow("SELECT mount_signal_uri FROM storage_info where id=?", i+1).Scan(&uri); err != nil {
			t.Fatal(err)
		}
		_ = uri
	}
}

// go test -bench=BenchmarkSqliteParallel -benchtime=10000x
func BenchmarkSqliteParallel(b *testing.B) {
	InitDB("./")
	db := GetDB()
	db.SetMaxOpenConns(1) // more than one should happen:database is locked
	// b.SetParallelism(12)
	b.SetParallelism(1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := db.Exec("INSERT INTO storage_info(max_size,mount_signal_uri)VALUES(?,?)", 34359738368*30*8, "127.0.0.1:/data/zfs"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
