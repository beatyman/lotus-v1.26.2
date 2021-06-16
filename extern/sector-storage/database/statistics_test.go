package database

import (
	"testing"
	"time"
)

func TestStatisWin(t *testing.T) {
	InitDB("./")
	now := time.Now()
	id := now.Format("20060102")
	if err := AddWinTimes(now, 1); err != nil {
		t.Fatal(err)
	}
	if err := AddWinGen(now, 0); err != nil {
		t.Fatal(err)
	}
	if err := AddWinSuc(now); err != nil {
		t.Fatal(err)
	}
	s, err := GetStatisWin(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.WinGen != s.WinSuc {
		t.Fatal(*s)
	}
}
