package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/gwaylib/errors"
)

type CpuCache struct {
	Cpu int
	L1d int
	L1i int
	L2  int
	L3  int
}

func GetTaskSetKey(group [][]int) map[int]bool {
	keys := map[int]bool{}
	for i, _ := range group {
		keys[i] = false
	}
	return keys
}

func GroupCpuCache(ctx context.Context) ([][]int, error) {
	out, err := exec.CommandContext(ctx, "lscpu", "-p=CPU,CACHE").CombinedOutput()
	if err != nil {
		return nil, errors.As(err)
	}
	r := csv.NewReader(bytes.NewReader(out))
	r.Comma = ','
	r.Comment = '#'
	records, err := r.ReadAll()
	if err != nil {
		return nil, errors.As(err)
	}

	group := [][]int{}
	cache := []int{}
	cacheIndex := 0
	for _, r := range records {
		if len(r) != 2 {
			return nil, errors.New("error cpu format").As(records)
		}
		cpu, err := strconv.Atoi(r[0])
		if err != nil {
			return nil, errors.As(err)
		}
		caches := strings.Split(r[1], ":")
		if len(caches) != 4 {
			return nil, errors.New("error cache format").As(records)
		}
		//		l1d, err := strconv.Atoi(caches[0])
		//		if err != nil {
		//			return nil, errors.New("error cache format").As(records)
		//		}
		//		l1i, err := strconv.Atoi(caches[1])
		//		if err != nil {
		//			return nil, errors.New("error cache format").As(records)
		//		}
		//		l2, err := strconv.Atoi(caches[2])
		//		if err != nil {
		//			return nil, errors.New("error cache format").As(records)
		//		}
		l3, err := strconv.Atoi(caches[3])
		if err != nil {
			return nil, errors.New("error cache format").As(records)
		}
		if l3 == cacheIndex {
			cache = append(cache, cpu)
		} else {
			group = append(group, cache)
			cache = []int{cpu}
			cacheIndex = l3
		}
	}
	return group, nil
}

var (
	cpuGroup = [][]int{}
	cpuKeys  = map[int]bool{}
	cpuLock  = sync.Mutex{}
)

func initCpuGroup(ctx context.Context) error {
	cpuLock.Lock()
	defer cpuLock.Unlock()
	if len(cpuKeys) == 0 {
		group, err := GroupCpuCache(ctx)
		if err != nil {
			return errors.As(err)
		}
		cpuKeys = GetTaskSetKey(group)
		cpuGroup = group
	}
	return nil
}

func allocateCpu(ctx context.Context) (int, []int, error) {
	if err := initCpuGroup(ctx); err != nil {
		return -1, nil, errors.As(err)
	}

	cpuLock.Lock()
	defer cpuLock.Unlock()
	for idx, using := range cpuKeys {
		if using {
			continue
		}
		cpuKeys[idx] = true
		return idx, cpuGroup[idx], nil
	}
	return -1, nil, errors.New("allocate cpu failed")
}

func returnCpu(key int) {
	cpuLock.Lock()
	defer cpuLock.Unlock()
	cpuKeys[key] = false
}
