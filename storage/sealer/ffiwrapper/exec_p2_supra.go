package ffiwrapper

import (
	"context"
	"fmt"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/hardware/bindcpu"
	"github.com/gwaylib/hardware/bindgpu"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

func execPrecommit2WithSupra(ctx context.Context, ak *bindgpu.GpuAllocateKey, gInfo *bindgpu.GpuInfo, cpuVal *bindcpu.CpuAllocateVal, sealer *Sealer, task WorkerTask) (storiface.SectorCids, error) {
	log.Infow("execPrecommit2WithSupra Start", "sector", task.SectorName())
	defer log.Infow("execPrecommit2WithSupra Finish", "sector", task.SectorName())
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecPrecommit2WithSupra panic: %v\n%v", e, string(debug.Stack()))
		}
	}()
	gpuKey := gInfo.Pci.PciBusID
	if strings.Index(gInfo.UUID, "GPU-") > -1 {
		uid := strings.TrimPrefix(gInfo.UUID, "GPU-")
		gpuKey = fmt.Sprintf("%s@%s", uid, gpuKey)
	}
	var cpus []string
	for _, cpuIndex := range cpuVal.Cpus {
		cpus = append(cpus, strconv.Itoa(cpuIndex))
	}
	orderCpuStr := strings.Join(cpus, ",")
	log.Infof("try bind CPU: %+v ,GPU: %+v", orderCpuStr, gpuKey)
	ssize, err := task.ProofType.SectorSize()
	if err != nil {
		return storiface.SectorCids{}, err
	}
	sealedPath := sealer.SectorPath("sealed", task.SectorName())
	cachePath := sealer.SectorPath("cache", task.SectorName())
	commDPath := filepath.Join(cachePath, "comm_d")
	commRPath := filepath.Join(cachePath, "comm_r")
	outPath := filepath.Join(cachePath, "sealed-file")
	program := fmt.Sprintf("./supra-p2-%v", ssize.ShortString())
	cmd := exec.CommandContext(ctx, program,
		"-d", sealedPath,
		"-i", cachePath,
		"-o", cachePath,
	)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("CUDA_VISIBLE_DEVICES=0"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return storiface.SectorCids{}, err
	}

	if err := bindcpu.BindCpuStr(cmd.Process.Pid, cpus); err != nil {
		cmd.Process.Kill()
		return storiface.SectorCids{}, err
	}
	StoreTaskPid(task.SectorName(), cmd.Process.Pid)
	defer FreeTaskPid(task.SectorName())
	if err := cmd.Wait(); err != nil {
		return storiface.SectorCids{}, err
	}
	commDBytes, err := os.ReadFile(commDPath)
	if err != nil {
		return storiface.SectorCids{}, err
	}
	commD, err := commcid.DataCommitmentV1ToCID(commDBytes)
	if err != nil {
		return storiface.SectorCids{}, err
	}
	commRBytes, err := os.ReadFile(commRPath)
	if err != nil {
		return storiface.SectorCids{}, err
	}
	commR, err := commcid.ReplicaCommitmentV1ToCID(commRBytes)
	if err != nil {
		return storiface.SectorCids{}, err
	}
	if err = exec.CommandContext(ctx, "mv", "-f", outPath, sealedPath).Run(); err != nil {
		return storiface.SectorCids{}, err
	}

	return storiface.SectorCids{
		Unsealed: commD,
		Sealed:   commR,
	}, nil
}
