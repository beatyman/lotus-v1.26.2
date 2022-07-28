package database

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkerUpdate(t *testing.T) {
	InitDB("./")

	info := &WorkerInfo{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		UpdateTime: time.Now(),
		Ip:         "1",
		Online:     true,
	}
	// expect insert.
	if err := OnlineWorker(info); err != nil {
		t.Fatal(err)
	}
	if _, err := GetWorkerInfo(info.ID); err != nil {
		t.Fatal(err)
	}

	info.Ip = "2"
	if err := OnlineWorker(info); err != nil {
		t.Fatal(err)
	}
	expect, err := GetWorkerInfo(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expect.Ip != info.Ip {
		t.Fatal(*expect, *info)
	}
	if !expect.Online {
		t.Fatal(*expect, *info)
	}
	if err := OfflineWorker(info.ID); err != nil {
		t.Fatal(err)
	}
	exp2, err := GetWorkerInfo(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exp2.Online {
		t.Fatal("expect off line")
	}
	if err := DisableWorker(info.ID, true); err != nil {
		t.Fatal(err)
	}
	exp3, err := GetWorkerInfo(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !exp3.Disable {
		t.Fatal("expect disabled")
	}
}
