package ffiwrapper

import (
	"context"
	"encoding/xml"
	"os/exec"
	"sync"

	"github.com/gwaylib/errors"
)

type GpuPci struct {
	PciBus string `xml:"pci_bus"`
	// TODO: more infomation
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
	input, err := exec.CommandContext(ctx, "nvidia-smi", "-q -x").CombinedOutput()
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
		using, _ := gpuKeys[gpuInfo.Pci.PciBus]
		if using {
			continue
		}
		gpuKeys[gpuInfo.Pci.PciBus] = true
		return gpuInfo.Pci.PciBus, &gpuInfo, nil
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
