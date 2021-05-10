package ffiwrapper

import (
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/gwaylib/errors"
)

type GpuPci struct {
	PciBus string `xml:"pci_bus"`
	// TODO: more infomation
}

func (p *GpuPci) ParseBusId() (int, error) {
	val, err := strconv.ParseInt(p.PciBus, 16, 32)
	if err != nil {
		return 0, err
	}
	return int(val), nil
}

type GpuInfo struct {
	Pci GpuPci `xml:"pci"`
	// TODO: more infomation
}

type GpuXml struct {
	XMLName xml.Name  `xml:"nvidia_smi_log"`
	Gpu     []GpuInfo `xml:"gpu"`
	// TODO: more infomation
}

func GroupGpu(ctx context.Context) ([]GpuInfo, error) {
	input, err := exec.CommandContext(ctx, "nvidia-smi", "-q", "-x").CombinedOutput()
	if err != nil {
		return nil, errors.As(err)
	}
	output := GpuXml{}
	if err := xml.Unmarshal(input, &output); err != nil {
		return nil, errors.As(err)
	}
	return output.Gpu, nil
}

var (
	gpuGroup  = []GpuInfo{}
	gpuKeys   = map[string]bool{}
	gpuLock   = sync.Mutex{}
	gpuInited = false
)

func initGpuGroup(ctx context.Context) error {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	if !gpuInited {
		gpuInited = true
		group, err := GroupGpu(ctx)
		if err != nil {
			return errors.As(err)
		}
		gpuGroup = group
	}
	return nil
}

func allocateGpu(ctx context.Context) (string, *GpuInfo, error) {
	if err := initGpuGroup(ctx); err != nil {
		return "", nil, errors.As(err)
	}

	gpuLock.Lock()
	defer gpuLock.Unlock()
	for _, gpuInfo := range gpuGroup {
		keyInt, err := gpuInfo.Pci.ParseBusId()
		if err != nil {
			log.Warn(errors.As(err))
			continue
		}
		key := strconv.Itoa(keyInt)
		using, _ := gpuKeys[key]
		if using {
			continue
		}
		gpuKeys[key] = true
		return key, &gpuInfo, nil
	}
	return "", nil, errors.New("allocate gpu failed").As(len(gpuKeys))
}

func returnGpu(key string) {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	if len(key) == 0 {
		return
	}

	gpuKeys[key] = false
}

// TODO: limit call frequency
func hasGPU(ctx context.Context) bool {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	gpus, err := GroupGpu(ctx)
	if err != nil {
		return false
	}
	return len(gpus) > 0
}

func AssertGPU(ctx context.Context) {
	// assert gpu for release mode
	// only the develop mode don't need gpu
	if os.Getenv("BELLMAN_NO_GPU") != "1" && os.Getenv("FIL_PROOFS_GPU_MODE") == "force" && !hasGPU(ctx) {
		log.Fatalf("os exit by gpu not found(BELLMAN_NO_GPU=%s, FIL_PROOFS_GPU_MODE=%s)", os.Getenv("BELLMAN_NO_GPU"), os.Getenv("FIL_PROOFS_GPU_MODE"))
		time.Sleep(3e9)
		os.Exit(-1)
	}
}
