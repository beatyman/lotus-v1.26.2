package main

import (
	"fmt"
	"testing"
)

func TestDiskPool(t *testing.T) {
	diskpool, err := NewDiskPool()
	if err != nil {
		panic(err)
	}

	for i := 0; i < 20; i++ {
		sid := fmt.Sprintf("s-t0%d", i)
		_, err := diskpool.Allocate(sid)
		if err != nil {
			panic(err)
		}
		fmt.Println(sid)
	}
	fmt.Println("print before delete")
	diskpool.Show()

	for i := 3; i < 20; i += 2 {
		sid := fmt.Sprintf("s-t0%d", i)
		diskpool.Delete(sid)
	}
	fmt.Println("print after delete")
	diskpool.Show()
}

func TestMain(m *testing.M) {
	fmt.Println("Test begins....")
	m.Run()
}

func TestDiskPoolAllocate(t *testing.T) {
	// diskNum := 4
	// taskPerDisk = 2
	tasks := []string{
		"s-t01000-0",
		"s-t01000-1",
		"s-t01000-2",
		"s-t01000-3",
		"s-t01000-4",
		"s-t01000-5",
		"s-t01000-6",
		"s-t01000-7",
	}

	pool, err := NewDiskPool()
	if err != nil {
		t.Fatal(err)
	}

	// allocate new task
	for idx, task := range tasks {
		_, err := pool.Allocate(task)
		if err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.

	// simulate task retry
	for idx, task := range tasks {
		_, err := pool.Allocate(task)
		if err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.

	// full allocate
	if _, err := pool.Allocate("s-t01000-8"); err == nil {
		t.Fatal("expect no disk to allocate")
	}

	// simulate restart process.
	rpool, err := NewDiskPool()
	if err != nil {
		t.Fatal(err)
	}
	// retry allocate task
	for idx, task := range tasks {
		_, err := rpool.Allocate(task)
		if err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.

	// test delete
	for idx, task := range tasks {
		if err := rpool.Delete(task); err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.

	// simulate new task
	tasks = []string{
		"s-t01000-100",
		"s-t01000-101",
		"s-t01000-102",
		"s-t01000-103",
		"s-t01000-104",
		"s-t01000-105",
		"s-t01000-106",
		"s-t01000-107",
	}
	for idx, task := range tasks {
		if _, err := pool.Allocate(task); err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	for idx, task := range tasks {
		if err := pool.Delete(task); err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.

	// simulate the miner changed
	tasks = []string{
		"s-t01001-100",
		"s-t01001-101",
		"s-t01001-102",
		"s-t01001-103",
		"s-t01001-104",
		"s-t01001-105",
		"s-t01001-106",
		"s-t01001-107",
	}
	for idx, task := range tasks {
		if _, err := rpool.Allocate(task); err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	for idx, task := range tasks {
		if err := rpool.Delete(task); err != nil {
			t.Fatalf("%s,%d,%s", err, idx, task)
		}
	}
	// TODO: checksum disk stat.
}
