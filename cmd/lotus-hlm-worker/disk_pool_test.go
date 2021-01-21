package main

import (
	"fmt"
	"os"
	"path"
	"testing"
)

func TestDiskPool(t *testing.T) {
	diskpool := NewDiskPool(2*KB, "./")

	for i := 10; i < 20; i++ {
		//fmt.Println("s-t0%d", i)
		sid := fmt.Sprintf("s-t0%d", i)
		sector_path, err := diskpool.Allocate(sid)
		if err != nil {
			panic(err)
		}

		sd := []string{"cache", "sealed", "unsealed"}
		for _, sdir := range sd {
			d := path.Join(sector_path, sdir)
			if _, err := os.Stat(d); os.IsNotExist(err) {
				os.Mkdir(d, os.ModePerm)
			}
			if _, err := os.Stat(path.Join(d, sid)); os.IsNotExist(err) {
				os.Mkdir(path.Join(d, sid), os.ModePerm)
			}
		}

		//defer os.Remove(file.Name())
		//fmt.Println(file.Name())
	}

	fmt.Println("------------------------before delete some sector")
	d, err := diskpool.ShowExt()
	if err == nil {
		fmt.Println(d)
	}

	fmt.Println("------------------------after delete some sector")
	for i := 10; i < 20; i += 2 {
		sid := fmt.Sprintf("s-t0%d", i)
		diskpool.Delete(sid)
	}
	d, err = diskpool.ShowExt()
	if err == nil {
		fmt.Println(d)
	}
}

//func TestMain(m *testing.M) {
//	fmt.Println("Test begins....")
//	m.Run()
//}

//func TestScanRepo(t *testing.T) {
//	result := scanRepo("/data/cache/.lotusworker")
//	fmt.Printf("%+v\n", result)
//}
