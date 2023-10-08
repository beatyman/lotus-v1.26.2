package ffiwrapper

import (
	"context"
	"github.com/gwaylib/errors"
	"github.com/shirou/gopsutil/cpu"
	"golang.org/x/sys/unix"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type CPUInfo struct {
	CPUID      int32
	L3Cache    int32
	NUMANode   int32
	TotalCores int32
	Allocated  bool
}

type ByCPUSortAscending []*CPUInfo
type ByCPUSortDescending []*CPUInfo

func (a ByCPUSortAscending) Len() int           { return len(a) }
func (a ByCPUSortAscending) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByCPUSortAscending) Less(i, j int) bool { return a[i].CPUID < a[j].CPUID }

func (a ByCPUSortDescending) Len() int           { return len(a) }
func (a ByCPUSortDescending) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByCPUSortDescending) Less(i, j int) bool { return a[i].CPUID > a[j].CPUID }

type CoreAllocator struct {
	coreInfos      []*CPUInfo
	allocatedCores map[int32]bool
	allocMutex     sync.Mutex
	initialized    uint32 // 0: not initialized, 1: initialized
	once           sync.Once
}

var globalAllocator = &CoreAllocator{}

func GetGlobalCoreAllocator() *CoreAllocator {
	if atomic.LoadUint32(&globalAllocator.initialized) == 0 {
		globalAllocator.once.Do(func() {
			infos, err := cpu.Info()
			if err != nil {
				log.Fatal(err)
				return
			}

			var coreInfos []*CPUInfo
			for _, info := range infos {
				PhysicalID, _ := strconv.Atoi(info.PhysicalID)
				coreInfos = append(coreInfos, &CPUInfo{
					CPUID:      info.CPU,
					L3Cache:    info.CacheSize,
					NUMANode:   int32(PhysicalID),
					TotalCores: info.Cores,
				})
			}
			//如果P2指定了固定CPU
			globalAllocator.coreInfos = coreInfos
			globalAllocator.allocatedCores = make(map[int32]bool)
			if list, ok := os.LookupEnv("FT_SupraSeal_P2_CPU_LIST"); ok && list != "" {
				cpus := strings.Split(strings.TrimSpace(list), ",")
				for _, c := range cpus {
					if cpuId, err := strconv.Atoi(c); err == nil {
						for i, info := range globalAllocator.coreInfos {
							if info.CPUID == int32(cpuId) {
								globalAllocator.coreInfos[i].Allocated = true
								globalAllocator.allocatedCores[info.CPUID] = true
							}
						}
					} else {
						log.Fatal(err)
					}
				}
			}
			atomic.StoreUint32(&globalAllocator.initialized, 1)
		})
	}
	return globalAllocator
}

func (ca *CoreAllocator) AllocateCores(numPhysicalCoresToAllocate int32, sortByDescending bool) ([]*CPUInfo, error) {
	ca.allocMutex.Lock()
	defer ca.allocMutex.Unlock()

	if sortByDescending {
		sort.Sort(ByCPUSortDescending(ca.coreInfos))
	} else {
		sort.Sort(ByCPUSortAscending(ca.coreInfos))
	}

	allocatedCores := make([]*CPUInfo, 0)
	allocatedCoresCount := int32(0)

	for _, info := range ca.coreInfos {
		if allocatedCoresCount >= numPhysicalCoresToAllocate {
			break
		}

		if !info.Allocated && !ca.allocatedCores[info.CPUID] && info.TotalCores > 0 {
			allocatedCores = append(allocatedCores, info)
			allocatedCoresCount += info.TotalCores
			info.Allocated = true
			ca.allocatedCores[info.CPUID] = true
		}
	}

	if allocatedCoresCount < numPhysicalCoresToAllocate {
		return nil, errors.New("not enough physical cores available")
	}
	sort.Sort(ByCPUSortAscending(allocatedCores))
	return allocatedCores, nil
}

func (ca *CoreAllocator) ReleaseCores(cores []*CPUInfo) {
	ca.allocMutex.Lock()
	defer ca.allocMutex.Unlock()

	for _, core := range cores {
		if core.Allocated {
			ca.allocatedCores[core.CPUID] = false
			for i, info := range ca.coreInfos {
				if info.CPUID == core.CPUID {
					ca.coreInfos[i].Allocated = false
				}
			}
		}
	}
}

func AllocateCpuForAP(ctx context.Context) ([]*CPUInfo, error) {
	cpucore := 2
	if os.Getenv("FT_AP_CORE_NUM") != "" {
		num, err := strconv.Atoi(os.Getenv("FT_AP_CORE_NUM"))
		if err != nil {
			return nil, errors.As(err)
		}
		cpucore = num
	}
	return GetGlobalCoreAllocator().AllocateCores(int32(cpucore), true)
}
func AllocateCpuForP1(ctx context.Context) ([]*CPUInfo, error) {
	cpucore := 2
	if os.Getenv("FT_P1_CORE_NUM") != "" {
		num, err := strconv.Atoi(os.Getenv("FT_P1_CORE_NUM"))
		if err != nil {
			return nil, errors.As(err)
		}
		cpucore = num
	}
	return GetGlobalCoreAllocator().AllocateCores(int32(cpucore), true)
}

func AllocateCpuForP2(ctx context.Context) ([]*CPUInfo, error) {
	infos, err := cpu.Info()
	if err != nil {
		return nil, errors.As(err)
	}
	var cpus []int32
	if list, ok := os.LookupEnv("FT_SupraSeal_P2_CPU_LIST"); ok && list != "" {
		list := strings.Split(strings.TrimSpace(list), ",")
		for _, cpuIndex := range list {
			cpuId, err := strconv.Atoi(cpuIndex)
			if err != nil {
				return nil, errors.As(err)
			}
			cpus = append(cpus, int32(cpuId))
		}
		var coreInfos []*CPUInfo
		for _, info := range infos {
			for _, cpuId := range cpus {
				if info.CPU == cpuId {
					physicalID, _ := strconv.Atoi(info.PhysicalID)
					coreInfos = append(coreInfos, &CPUInfo{
						CPUID:      info.CPU,
						L3Cache:    info.CacheSize,
						NUMANode:   int32(physicalID),
						TotalCores: info.Cores,
					})
				}
			}
		}
		return coreInfos, nil
	}
	cpucore := 8
	if os.Getenv("FT_P2_CORE_NUM") != "" {
		num, err := strconv.Atoi(os.Getenv("FT_P2_CORE_NUM"))
		if err != nil {
			return nil, errors.As(err)
		}
		cpucore = num
	}
	return GetGlobalCoreAllocator().AllocateCores(int32(cpucore), false)
}

// pin golang thread
// TODO: no effect now
func BindCpu(pid int, cpus []int) error {
	// set cpu affinity
	cpuSet := unix.CPUSet{}
	for _, cpu := range cpus {
		cpuSet.Set(cpu)
	}
	// https://github.com/golang/go/issues/11243
	if err := unix.SchedSetaffinity(pid, &cpuSet); err != nil {
		return errors.As(err)
	}
	return nil
}

// pin golang thread
// TODO: no effect now
func BindCpuStr(pid int, cpus []string) error {
	bind := make([]int, len(cpus))
	for i, c := range cpus {
		ci, err := strconv.Atoi(c)
		if err != nil {
			return errors.As(err, cpus, i)
		}
		bind[i] = ci
	}
	return BindCpu(pid, bind)
}