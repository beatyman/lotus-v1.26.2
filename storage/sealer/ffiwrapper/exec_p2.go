package ffiwrapper

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ExecPrecommit2Req struct {
	Repo string
	Task WorkerTask
}

type ExecPrecommit2Resp struct {
	Data storiface.SectorCids
	Err  string
}

func (sb *Sealer) ExecPreCommit2(ctx context.Context, task WorkerTask) (storiface.SectorCids, error) {
	taskConfig, err := GetGlobalResourceManager().AllocateResource(P2Task)
	if err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}
	defer GetGlobalResourceManager().ReleaseResource(taskConfig)
	log.Infof("try bind CPU: %+v ,GPU: %+v", taskConfig.CPUSet, taskConfig.GPU)
	if useSupra, ok := os.LookupEnv("X_USE_SupraSeal"); ok && strings.ToLower(useSupra) != "false" {
		return execPrecommit2WithSupra(ctx, taskConfig, sb, task)
	} else {
		return execPrecommit2WithLotus(ctx, taskConfig, sb, task)
	}
}
func execPrecommit2WithLotus(ctx context.Context, taskConfig ResourceUnit, sb *Sealer, task WorkerTask) (storiface.SectorCids, error) {
	log.Infow("execPrecommit2WithLotus Start", "sector", task.SectorName())
	defer log.Infow("execPrecommit2WithLotus Finish", "sector", task.SectorName())
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("execPrecommit2WithLotus panic: %v\n%v", e, string(debug.Stack()))
		}
	}()
	orderCpu := strings.Split(taskConfig.CPUSet, ",")
	unixAddr := filepath.Join(os.TempDir(), fmt.Sprintf(".p2-%s-%s-%s", task.Key(), taskConfig.GPU, taskConfig.CPUSet))
	cmd := exec.CommandContext(ctx, os.Args[0],
		"precommit2",
		"--addr", unixAddr,
	)
	gpus := strings.Split(taskConfig.GPU, ",")
	if len(gpus) < 1 {
		return storiface.SectorCids{}, errors.New("You need to configure the P2 GPU in the configuration file")
	}
	//zdz P2 可以多卡吗？
	gpuIndex, err := strconv.Atoi(gpus[0])
	if err != nil {
		return storiface.SectorCids{}, errors.As(err, fmt.Sprintf("wrong config %+v", gpus))
	}
	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU=%s", getHlmGPUInfo(ctx, gpuIndex)))
	cmd.Env = append(cmd.Env, fmt.Sprintf("LOTUS_EXEC_CODE=%s", _exec_code))
	//cmd.Env = append(cmd.Env, fmt.Sprintf("RUST_BACKTRACE=%s", "full"))
	//cmd.Env = append(cmd.Env, fmt.Sprintf("RUST_LOG=%s", "trace"))
	if len(os.Getenv("NVIDIA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%s", taskConfig.GPU))
	}
	if len(os.Getenv("CUDA_VISIBLE_DEVICES")) == 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CUDA_VISIBLE_DEVICES=%s", taskConfig.GPU))
	}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}
	if err := BindCpuStr(cmd.Process.Pid, orderCpu); err != nil {
		cmd.Process.Kill()
		return storiface.SectorCids{}, errors.As(err)
	}
	StoreTaskPid(task.SectorName(), cmd.Process.Pid)
	defer FreeTaskPid(task.SectorName())
	log.Infof("DEBUG: bind precommit2:%+v, process:%d, gpu:%+v,cpus:%+v,env: %+v", task.Key(), cmd.Process.Pid, taskConfig.GPU, taskConfig.CPUSet, cmd.Env)
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
		return storiface.SectorCids{}, errors.As(err)
	}
	// will block the data
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug(errors.As(err))
		}
	}()

	args, err := json.Marshal(&ExecPrecommit2Req{
		Repo: sb.RepoPath(),
		Task: task,
	})
	if err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}

	// write args
	encryptArg, err := AESEncrypt(args, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	if _, err := conn.Write(encryptArg); err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}

	// wait resp
	out, err := readUnixConn(conn)
	if err != nil {
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
