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
	tasks := []string{
		"s-t01000-0",
		"s-t01000-1",
		"s-t01000-2",
		"s-t01000-3",
		"s-t01000-4",
		"s-t01000-5",
		"s-t01000-6",
		"s-t01000-7",
		"s-t01000-8",
		"s-t01000-9",
		"s-t01000-10",
		"s-t01000-11",
		"s-t01000-12",
		"s-t01000-13",
		"s-t01000-14",
		"s-t01000-15",
		"s-t01000-16",
		"s-t01000-17",
		"s-t01000-18",
		"s-t01000-19",
		"s-t01000-20",
		"s-t01000-21",
		"s-t01000-22",
		"s-t01000-23",
		"s-t01000-24",
		"s-t01000-25",
		"s-t01000-26",
		"s-t01000-27",
		"s-t01000-28",
		"s-t01000-29",
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
		"s-t01000-108",
		"s-t01000-109",
		"s-t01000-110",
		"s-t01000-111",
		"s-t01000-112",
		"s-t01000-113",
		"s-t01000-114",
		"s-t01000-115",
		"s-t01000-116",
		"s-t01000-117",
		"s-t01000-118",
		"s-t01000-119",
		"s-t01000-120",
		"s-t01000-121",
		"s-t01000-122",
		"s-t01000-123",
		"s-t01000-124",
		"s-t01000-125",
		"s-t01000-126",
		"s-t01000-127",
		"s-t01000-128",
		"s-t01000-129",
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
		"s-t01001-108",
		"s-t01001-109",
		"s-t01001-110",
		"s-t01001-111",
		"s-t01001-112",
		"s-t01001-113",
		"s-t01001-114",
		"s-t01001-115",
		"s-t01001-116",
		"s-t01001-117",
		"s-t01001-118",
		"s-t01001-119",
		"s-t01001-120",
		"s-t01001-121",
		"s-t01001-122",
		"s-t01001-123",
		"s-t01001-124",
		"s-t01001-125",
		"s-t01001-126",
		"s-t01001-127",
		"s-t01001-128",
		"s-t01001-129",
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
