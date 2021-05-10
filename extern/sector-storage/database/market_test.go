package database

import (
	"testing"
	"time"
)

func TestMarketRetrieve(t *testing.T) {
	InitDB("./")
	sid := "s-t01001-1"
	now := time.Now()
	// clean the expired
	if _, err := ExpireAllMarketRetrieve(now.AddDate(0, -1, 0), ""); err != nil {
		t.Fatal(err)
	}

	// init
	if err := AddMarketRetrieve(sid); err != nil {
		t.Fatal(err)
	}

	// insert success
	if _, err := GetMarketRetrieve(sid); err != nil {
		t.Fatal(err)
	}

	// update
	if err := AddMarketRetrieve(sid); err != nil {
		t.Fatal(err)
	}

	// no overdue
	if expired, err := GetExpireMarketRetrieve(now.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	} else if len(expired) > 0 {
		t.Fatal(err)
	}

	// set the expired.
	db := GetDB()

	if _, err := db.Exec("UPDATE market_retrieve SET retrieve_time=? WHERE sid=?", now.AddDate(-1, 0, 0), sid); err != nil {
		t.Fatal(err)
	}

	if expired, err := GetExpireMarketRetrieve(now.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	} else if len(expired) != 1 {
		t.Fatal("expect 1 expired")
	} else if expired[0].SectorId != sid {
		t.Fatal("expect expired:" + sid)
	}
	if err := ExpireMarketRetrieve(sid); err != nil {
		t.Fatal(err)
	}
	if expired, err := GetExpireMarketRetrieve(now.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	} else if len(expired) > 0 {
		t.Fatal("expect no expired")
	}
}
