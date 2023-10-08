package ffiwrapper

import (
	"context"
	"fmt"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
)

func execPrecommit2WithSupra(ctx context.Context, taskConfig ResourceUnit ,sb *Sealer, task WorkerTask) (storiface.SectorCids, error) {
	log.Infow("execPrecommit2WithSupra Start", "sector", task.SectorName())
	defer log.Infow("execPrecommit2WithSupra Finish", "sector", task.SectorName())
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecPrecommit2WithSupra panic: %v\n%v", e, string(debug.Stack()))
		}
	}()
	orderCpu := strings.Split(taskConfig.CPUSet,",")
	log.Infof("try bind CPU: %+v ,GPU: %+v", taskConfig.CPUSet, taskConfig.GPU)
	ssize, err := task.ProofType.SectorSize()
	if err != nil {
		return storiface.SectorCids{}, err
	}
	sealedPath := sb.SectorPath("sealed", task.SectorName())
	cachePath := sb.SectorPath("cache", task.SectorName())
	commDPath := filepath.Join(cachePath, "comm_d")
	commRPath := filepath.Join(cachePath, "comm_r")
	outPath := filepath.Join(cachePath, "sealed-file")
	cfgPath := filepath.Join(os.Getenv("PRJ_ROOT"), "etc", "supra_seal.cfg")
	program := fmt.Sprintf("./supra-p2-%v", ssize.ShortString())
	cmd := exec.CommandContext(ctx, program,
		"-d", sealedPath,
		"-i", cachePath,
		"-o", cachePath,
		"-c", cfgPath,
	)
	cmd.Env = os.Environ()
	if len(os.Getenv("NVIDIA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%s", taskConfig.GPU))
	}
	if len(os.Getenv("CUDA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CUDA_VISIBLE_DEVICES=%s", taskConfig.GPU))
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return storiface.SectorCids{}, err
	}

	if err := BindCpuStr(cmd.Process.Pid, orderCpu); err != nil {
		cmd.Process.Kill()
		return storiface.SectorCids{}, err
	}
	StoreTaskPid(task.SectorName(), cmd.Process.Pid)
	defer FreeTaskPid(task.SectorName())
	if err := cmd.Wait(); err != nil {
		return storiface.SectorCids{}, err
	}
	log.Infof("DEBUG: bind precommit2:%+v, process:%d, gpu:%+v,cpus:%+v,env: %+v",task.Key(), cmd.Process.Pid,taskConfig.GPU, taskConfig.CPUSet,cmd.Env)
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