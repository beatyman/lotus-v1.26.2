package database

import (
	"testing"
	"time"
)

func TestStatisWin(t *testing.T) {
	InitDB("./")
	id := time.Now().Format("20060102")
	if err := AddWinTimes(id); err != nil {
		t.Fatal(err)
	}
	if err := AddWinGen(id); err != nil {
		t.Fatal(err)
	}
	if err := AddWinSuc(id); err != nil {
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
