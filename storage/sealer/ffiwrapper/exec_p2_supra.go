package ffiwrapper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/hardware/bindcpu"
	"github.com/gwaylib/hardware/bindgpu"
	"golang.org/x/xerrors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

func execPrecommit2WithSupra(ctx context.Context, ak *bindgpu.GpuAllocateKey, gInfo *bindgpu.GpuInfo,cpuKeys []*bindcpu.CpuAllocateKey, cpuVal *bindcpu.CpuAllocateVal, sealer *Sealer, task WorkerTask) (storiface.SectorCids, error) {
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
	if list, ok := os.LookupEnv("FT_SupraSeal_P2_CPU_LIST"); ok && list !="" {
		bindcpu.ReturnCpus(cpuKeys)
		cpus=strings.Split(strings.TrimSpace(list),",")
	}else {
		for _, cpuIndex := range cpuVal.Cpus {
			cpus = append(cpus, strconv.Itoa(cpuIndex))
		}
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
		cmd.Env = append(cmd.Env, fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%d", ak.Index))
	}
	if len(os.Getenv("CUDA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CUDA_VISIBLE_DEVICES=%d", ak.Index))
	}
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
	//添加一个P2做完的check
	p1odec := map[string]interface{}{}

	if err := json.Unmarshal(task.PreCommit1Out, &p1odec); err != nil {
		return storiface.SectorCids{}, xerrors.Errorf("unmarshaling pc1 output: %w", err)
	}

	var ticket abi.SealRandomness
	ti, found := p1odec["_lotus_SealRandomness"]

	if found {
		ticket, err = base64.StdEncoding.DecodeString(ti.(string))
		if err != nil {
			return storiface.SectorCids{}, xerrors.Errorf("decoding ticket: %w", err)
		}

		for i := 0; i < PC2CheckRounds; i++ {
			log.Infof("path: %+v,sealedCID: %+v,unsealedCID: %+v,sector:%+v,ssize:%+v",sealer.RepoPath(),commR,commD,task.SectorName(),ssize)
			var sd [32]byte
			_, _ = rand.Read(sd[:])

			_, err := ffi.SealCommitPhase1(
				task.ProofType,
				commR,
				commD,
				cachePath,
				sealedPath,
				task.SectorID.Number,
				task.SectorID.Miner,
				ticket,
				sd[:],
				[]abi.PieceInfo{{Size: abi.PaddedPieceSize(ssize), PieceCID: commD}},
			)
			if err != nil {
				log.Warn("checking PreCommit failed: ", err)
				log.Warnf("num:%d tkt:%v seed:%v sealedCID:%v, unsealedCID:%v", task.SectorID.Number, ticket, sd[:], commR, commD)

				return storiface.SectorCids{}, xerrors.Errorf("checking PreCommit failed: %w", err)
			}
		}
	}
	if err = exec.CommandContext(ctx, "mv", "-f", outPath, sealedPath).Run(); err != nil {
		return storiface.SectorCids{}, err
	}

	return storiface.SectorCids{
		Unsealed: commD,
		Sealed:   commR,
	}, nil
}
