package ffiwrapper

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/hardware/bindcpu"
	"github.com/gwaylib/hardware/bindgpu"
	"github.com/urfave/cli/v2"
)

type ExecPrecommit2Req struct {
	Repo string
	Task WorkerTask
}

type ExecPrecommit2Resp struct {
	Data storiface.SectorCids
	Err  string
}

func (sb *Sealer) bindP2Process(ctx context.Context, ak *bindgpu.GpuAllocateKey, gInfo *bindgpu.GpuInfo, cpuVal *bindcpu.CpuAllocateVal, task WorkerTask) error {
	l3CpuNum := len(cpuVal.Cpus)
	gpuKey := gInfo.Pci.PciBusID
	if strings.Index(gInfo.UUID, "GPU-") > -1 {
		uid := strings.TrimPrefix(gInfo.UUID, "GPU-")
		gpuKey = fmt.Sprintf("%s@%s", uid, gpuKey)
	}
	orderCpu := []string{}
	for _, cpu := range cpuVal.Cpus {
		orderCpu = append(orderCpu, strconv.Itoa(cpu))
	}
	orderCpuStr := strings.Join(orderCpu, ",")
	log.Infof("try bind CPU: %+v ,GPU: %+v", orderCpuStr, gpuKey)
	unixAddr := filepath.Join(os.TempDir(), fmt.Sprintf(".p2-%s-%s", gInfo.UniqueID(), orderCpuStr))
	cmd := exec.CommandContext(ctx, os.Args[0],
		"precommit2",
		"--addr", unixAddr,
	)
	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU=%s", gpuKey))
	cmd.Env = append(cmd.Env, fmt.Sprintf("LOTUS_EXEC_CODE=%s", _exec_code))
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU_IDX=%d", ak.Index))
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU=%s", gInfo.UniqueID()))
	if l3CpuNum < 2 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FILECOIN_P2_CPU_NUM=%d", 2))
	} else {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FILECOIN_P2_CPU_NUM=%d", l3CpuNum))
	}
	if len(os.Getenv("NVIDIA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%d", ak.Index))
	}
	if len(os.Getenv("CUDA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CUDA_VISIBLE_DEVICES=%d", ak.Index))
	}

	// output the stderr log
	//cmd.Stderr = &P2Stderr{}
	//cmd.Stdout = &P2Stdout{}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return errors.As(err)
	}
	var cpus []string
	for _, cpuIndex := range cpuVal.Cpus {
		cpus = append(cpus, strconv.Itoa(cpuIndex))
	}
	if err := bindcpu.BindCpuStr(cmd.Process.Pid, cpus); err != nil {
		cmd.Process.Kill()
		return errors.As(err)
	}
	StoreTaskPid(task.SectorName(), cmd.Process.Pid)

	log.Infof("DEBUG: bind precommit2, process:%d, gpu:%+v,cpus:%+v", cmd.Process.Pid, cmd.Env, cpuVal.Cpus)

	// transfer precommit1 parameters
	var d net.Dialer
	d.LocalAddr = nil // if you have a local addr, add it here
	retryTime := 0
	raddr := net.UnixAddr{Name: unixAddr, Net: "unix"}
loopUnixConn:
	conn, err := d.DialContext(ctx, "unix", raddr.String())
	if err != nil {
		retryTime++
		if retryTime < 100 {
			time.Sleep(200 * time.Millisecond)
			goto loopUnixConn
		}
		cmd.Process.Kill()
		return errors.As(err, raddr.String())
	}
	gInfo.SetConn(ak.Thread, conn)

	// will block the data
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug(errors.As(err))
		}
	}()
	return nil
}

func (sb *Sealer) ExecPreCommit2(ctx context.Context, task WorkerTask) (storiface.SectorCids, error) {
	bindgpu.AssertGPU(ctx)

	gpuKey, gpuInfo := bindgpu.SyncAllocateGPU(ctx)
	defer bindgpu.ReturnGPU(gpuKey)

	cpuKeys, cpuVal, err := AllocateCpuForP2(ctx)
	if err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}
	defer bindcpu.ReturnCpus(cpuKeys)

	if useSupra, ok := os.LookupEnv("X_USE_SupraSeal"); ok && strings.ToLower(useSupra) != "false" {
		return execPrecommit2WithSupra(ctx, gpuKey, gpuInfo, cpuVal, sb, task)
	}
	if gpuInfo.UniqueID() == "nogpu" {
		sref := storiface.SectorRef{
			ID:        task.SectorID,
			ProofType: task.ProofType,
			SectorFile: storiface.SectorFile{
				SectorId:       storiface.SectorName(task.SectorID),
				SealedRepo:     sb.RepoPath(),
				UnsealedRepo:   sb.RepoPath(),
				IsMarketSector: task.SectorStorage.UnsealedStorage.ID != 0,
			},
			StoreUnseal:        task.StoreUnseal,
			SectorRepairStatus: task.SectorRepairStatus,
		}
		return sb.sealPreCommit2(ctx, sref, task.PreCommit1Out)
	}

	args, err := json.Marshal(&ExecPrecommit2Req{
		Repo: sb.RepoPath(),
		Task: task,
	})
	if err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}

	// bind gpu
	conn := gpuInfo.GetConn(gpuKey.Thread)
	if conn == nil {
		if err := sb.bindP2Process(ctx, gpuKey, gpuInfo, cpuVal, task); err != nil {
			gpuInfo.Close(gpuKey.Thread)
			return storiface.SectorCids{}, errors.As(err)
		}
	}
	defer FreeTaskPid(task.SectorName())
	conn = gpuInfo.GetConn(gpuKey.Thread)

	// write args
	encryptArg, err := AESEncrypt(args, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		gpuInfo.Close(gpuKey.Thread)
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	if _, err := conn.Write(encryptArg); err != nil {
		gpuInfo.Close(gpuKey.Thread)
		return storiface.SectorCids{}, errors.As(err, string(args))
	}

	// wait resp
	out, err := readUnixConn(conn)
	if err != nil {
		gpuInfo.Close(gpuKey.Thread)
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	decodeOut, err := AESDecrypt(out, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	resp := ExecPrecommit2Resp{}
	if err := json.Unmarshal(decodeOut, &resp); err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args), string(out))
	}
	if len(resp.Err) > 0 {
		return storiface.SectorCids{}, errors.Parse(resp.Err).As(string(args), resp)
	}
	return resp.Data, nil
}

var P2Cmd = &cli.Command{
	Name:  "precommit2",
	Usage: "run precommit2 in process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "addr", // listen address
		},
	},
	Action: func(cctx *cli.Context) error {
		localCode := os.Getenv("LOTUS_EXEC_CODE")
		if len(localCode) == 0 {
			return nil
		}

		//	ctx := cctx.Context
		ctx := context.Background()

		unixFile := cctx.String("addr")
		if err := os.Remove(unixFile); err != nil {
			log.Info(errors.As(err))
		}

		unixAddr := net.UnixAddr{Name: unixFile, Net: "unix"}

		// unix listen
		ln, err := net.ListenUnix("unix", &unixAddr)
		if err != nil {
			return errors.As(err)
		}
		defer ln.Close()

		conn, err := ln.Accept()
		if err != nil {
			return errors.As(err)
		}
		defer conn.Close()

		workLock := sync.Mutex{}
		work := func(argIn []byte) {
			workLock.Lock()
			defer workLock.Unlock()

			resp := ExecPrecommit2Resp{}
			defer func() {
				if r := recover(); r != nil {
					resp.Err = errors.New("panic").As(r).Error()
				}
				result, err := json.Marshal(&resp)
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				encryptOut, err := AESEncrypt(result, fmt.Sprintf("%x", md5.Sum([]byte(localCode))))
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				if _, err := conn.Write(encryptOut); err != nil {
					log.Error(errors.As(err, string(result)))
					os.Exit(2)
					return
				}
			}()

			req := ExecPrecommit2Req{}
			if err := json.Unmarshal(argIn, &req); err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}

			workerRepo := req.Repo
			task := req.Task

			log.Infof("p2 process req in:%s, %s, %+v", unixAddr.String(), workerRepo, task.SectorID)
			workerSealer, err := New(RemoteCfg{}, &basicfs.Provider{
				Root: workerRepo,
			})
			if err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}
			sref := storiface.SectorRef{
				ID:        task.SectorID,
				ProofType: task.ProofType,
				SectorFile: storiface.SectorFile{
					SectorId:       storiface.SectorName(task.SectorID),
					SealedRepo:     workerRepo,
					UnsealedRepo:   workerRepo,
					IsMarketSector: task.SectorStorage.UnsealedStorage.ID != 0,
				},
				StoreUnseal:        task.StoreUnseal,
				SectorRepairStatus: task.SectorRepairStatus,
			}
			out, err := workerSealer.SealPreCommit2(ctx, sref, task.PreCommit1Out)
			if err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}
			resp.Data = out
			return
		}
		for {
			// handle connection
			argIn, err := readUnixConn(conn)
			if err != nil {
				if errors.Equal(err, io.EOF) {
					return nil
				}
				return errors.As(err)
			}

			decodeArg, err := AESDecrypt(argIn, fmt.Sprintf("%x", md5.Sum([]byte(localCode))))
			if err != nil {
				return errors.As(err)
			}
			go work(decodeArg)
		}
		return nil
	},
}
