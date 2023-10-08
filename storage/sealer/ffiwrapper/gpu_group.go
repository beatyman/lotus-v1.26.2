package ffiwrapper

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/gwaylib/errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GpuPci struct {
	PciBus   string `xml:"pci_bus"`
	PciBusID string `xml:"pci_bus_id"`
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
	UUID string `xml:"uuid"`
	Pci  GpuPci `xml:"pci"`
	// TODO: more infomation
}

func (info *GpuInfo) UniqueID() string {
	uid := strings.Replace(info.UUID, "GPU-", "", 1)
	if len(uid) == 0 {
		return "nogpu"
	}
	return uid
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
	gpuGroup []GpuInfo
	gpuKeys  = map[string]string{}
	gpuLock  = sync.Mutex{}
)

type GpuAllocateKey struct {
	Index  int
	Thread int
}

func (g *GpuAllocateKey) Key() string {
	return fmt.Sprintf("%d_%d", g.Index, g.Thread)
}

func initGpuGroup(ctx context.Context) error {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	if gpuGroup == nil {
		group, err := GroupGpu(ctx)
		if err != nil {
			log.Warn(errors.As(err))
		}
		gpuGroup = group
	}
	return nil
}

// TODO: limit call frequency
func HasGPU(ctx context.Context) bool {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	gpus, err := GroupGpu(ctx)
	if err != nil {
		return false
	}
	return len(gpus) > 0
}

func NeedGPU() bool {
	return os.Getenv("BELLMAN_NO_GPU") != "1" && os.Getenv("FIL_PROOFS_GPU_MODE") == "force"
}

func AllocateGPU(ctx context.Context) (*GpuAllocateKey, *GpuInfo, error) {
	if err := initGpuGroup(ctx); err != nil {
		log.Warn(errors.As(err))
		//return nil, nil, errors.As(err)
	}

	gpuLock.Lock()
	defer gpuLock.Unlock()
	maxThread := 1
	for i := 0; i < maxThread; i++ {
		// range is a copy
		for j, gpuInfo := range gpuGroup {
			ak := &GpuAllocateKey{
				Index:  j,
				Thread: i,
			}
			key := ak.Key()
			using, _ := gpuKeys[key]
			if len(using) > 0 {
				continue
			}
			gpuKeys[key] = gpuInfo.UniqueID()
			return ak, &gpuGroup[j], nil
		}
	}
	if len(gpuGroup) == 0 || !NeedGPU() {
		// using cpu when no gpu hardware
		return &GpuAllocateKey{}, &GpuInfo{}, nil
	}
	return nil, nil, errors.New("allocate gpu failed").As(gpuKeys, gpuGroup)
}

func SyncAllocateGPU(ctx context.Context) (*GpuAllocateKey, *GpuInfo) {
	for {
		ak, group, err := AllocateGPU(ctx)
		if err != nil {
			log.Warn("allocate gpu failed, retry 1s later. ", errors.As(err))
			time.Sleep(1e9)
			continue
		}
		return ak, group
	}
}

func ReturnGPU(key *GpuAllocateKey) {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	if key == nil {
		return
	}
	gpuKeys[key.Key()] = ""
}

func AssertGPU(ctx context.Context) {
	// assert gpu for release mode
	// only the develop mode don't need gpu
	if NeedGPU() && !HasGPU(ctx) {
		log.Fatalf("os exit by gpu not found")
		time.Sleep(3e9)
		os.Exit(-1)
	}
}

// bind gpu, if 'NEPTUNE_DEFAULT_GPU_IDX' is set
func bindGPU(ctx context.Context, idx int) {
	if err := initGpuGroup(ctx); err != nil {
		log.Warn(errors.As(err, idx))
		return
	}

	gpuLock.Lock()
	defer gpuLock.Unlock()
	if len(gpuGroup) <= idx {
		log.Warn(errors.New("idx is out of gpu pool"))
		return
	}
	gpuInfo := gpuGroup[idx]

	os.Setenv("NEPTUNE_DEFAULT_GPU", gpuInfo.UniqueID())
	// https://filecoinproject.slack.com/archives/CR98WERRN/p1672367336271889
	if len(os.Getenv("NVIDIA_VISIBLE_DEVICES")) == 0 {
		os.Setenv("NVIDIA_VISIBLE_DEVICES", strconv.Itoa(idx))
	}
	if len(os.Getenv("CUDA_VISIBLE_DEVICES")) == 0 {
		os.Setenv("CUDA_VISIBLE_DEVICES", strconv.Itoa(idx))
	}
}

func BindGPU(ctx context.Context) {
	defaultGPUEnv := os.Getenv("NEPTUNE_DEFAULT_GPU")
	if len(defaultGPUEnv) > 0 {
		// alreay set
		return
	}

	envIdxStr := os.Getenv("NEPTUNE_DEFAULT_GPU_IDX")
	if len(envIdxStr) == 0 {
		// GPU_IDX not set, using default
		return
	}

	envIdx, err := strconv.Atoi(envIdxStr)
	if err != nil {
		log.Warn(errors.As(err, envIdxStr))
		return
	}
	bindGPU(ctx, envIdx)
	log.Infof("DEBUG: bind gpu, %d", envIdx)
}
//zdz rust底层特殊处理
func getHlmGPUInfo(ctx context.Context, idx int) string {
	key:=fmt.Sprintf("%d", idx)
	if err := initGpuGroup(ctx); err != nil {
		log.Warn(errors.As(err))
		return key
	}
	gpuLock.Lock()
	defer gpuLock.Unlock()
	if len(gpuGroup) <= idx {
		log.Warn(errors.New("idx is out of gpu pool"))
		return key
	}
	gpuInfo := gpuGroup[idx]
	if strings.Index(gpuInfo.UUID, "GPU-") > -1 {
		uid := strings.TrimPrefix(gpuInfo.UUID, "GPU-")
		key = fmt.Sprintf("%s@%d", uid, idx)
	}
	return key
}