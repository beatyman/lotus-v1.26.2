package main

import (
	"fmt"
	"testing"
)

func TestDiskPool(t *testing.T) {
	diskpool, _ := NewDiskPool()
	for i := 0; i < 20; i++ {
		//fmt.Println("s-t0%d", i)
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
