package ffiwrapper

import (
	"context"
	"github.com/gwaylib/hardware/bindcpu"
	"os"
	"strconv"
)

func AllocateCpuForAP(ctx context.Context) ([]*bindcpu.CpuAllocateKey, *bindcpu.CpuAllocateVal,error) {
	cpucore:=2
	if os.Getenv("FT_AP_CORE_NUM")!=""{
		num, err := strconv.Atoi(os.Getenv("FT_AP_CORE_NUM"))
		if err != nil {
			return nil, nil,err
		}
		cpucore=num
	}

	keys, val, err := bindcpu.AllocateFixedCpu(ctx, cpucore, false)
	if err != nil {
		return nil, nil,err
	}
	return keys,val,err
}
func OrderAllCpuForP1(ctx context.Context) (bindcpu.CpuGroupInfo, string, error) {
	groupLen := bindcpu.CpuGroupL3Thread(ctx)
	return bindcpu.OrderAllCpu(ctx, 0, groupLen, false)
}
func AllocateCpuForP1(ctx context.Context) ([]*bindcpu.CpuAllocateKey, *bindcpu.CpuAllocateVal,error) {
	cpucore:=2
	if os.Getenv("FT_P1_CORE_NUM")!=""{
		num, err := strconv.Atoi(os.Getenv("FT_P1_CORE_NUM"))
		if err != nil {
			return nil, nil,err
		}
		cpucore=num
	}

	keys, val, err := bindcpu.AllocateFixedCpu(ctx, cpucore, false)
	if err != nil {
		return nil, nil,err
	}
	return keys,val,err
}

func AllocateCpuForP2(ctx context.Context) ([]*bindcpu.CpuAllocateKey, *bindcpu.CpuAllocateVal,error) {
	cpucore:=8
	if os.Getenv("FT_P2_CORE_NUM")!=""{
		num, err := strconv.Atoi(os.Getenv("FT_P2_CORE_NUM"))
		if err != nil {
			return nil, nil,err
		}
		cpucore=num
	}
	keys, val, err := bindcpu.AllocateFixedCpu(ctx, cpucore, true)
	if err != nil {
		return nil, nil,err
	}
	return keys,val,err
}