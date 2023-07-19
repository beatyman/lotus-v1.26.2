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
	"github.com/urfave/cli/v2"
)

type ExecP1Req struct {
	Repo string
	Task WorkerTask
}

type ExecP1Resp struct {
	Data storiface.PreCommit1Out
	Err  string
}

func (sb *Sealer)bindP1Process(ctx context.Context, cpuKeys []*bindcpu.CpuAllocateKey, cpuVal *bindcpu.CpuAllocateVal,task WorkerTask) error {
	orderCpu := []string{}
	for _, cpu := range cpuVal.Cpus {
		orderCpu = append(orderCpu, strconv.Itoa(cpu))
	}
	orderCpuStr := strings.Join(orderCpu, ",")
	unixAddr := filepath.Join(os.TempDir(), fmt.Sprintf(".p1-%s", orderCpuStr))

	cmd := exec.CommandContext(ctx, os.Args[0],
		"precommit1",
		"--addr", unixAddr,
	)

	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("LOTUS_EXEC_CODE=%s", _exec_code))
	cmd.Env = append(cmd.Env, fmt.Sprintf("FILECOIN_P1_CORES=%s", orderCpuStr))
	cmd.Env = append(cmd.Env, fmt.Sprintf("FILECOIN_P1_CORES_LEN=%d", len(orderCpu)))
	if len(orderCpu) < 2 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_USE_MULTICORE_SDR=0"))
	} else {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_MULTICORE_SDR_PRODUCERS=%d", len(orderCpu)-1))
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_USE_MULTICORE_SDR=1"))
	}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return errors.As(err)
	}
	if err := bindcpu.BindCpu(cmd.Process.Pid, cpuVal.Cpus); err != nil {
		cmd.Process.Kill()
		return errors.As(err)
	}
	StoreTaskPid(task.SectorName(),cmd.Process.Pid)
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
	cpuVal.Conn = conn
	//bindcpu.BindCpuConn(cpuKeys, cpuVal)

	// will block the data
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug(errors.As(err))
		}
	}()
	log.Infof("DEBUG: bind precommit1, process:%d, cpus:%s", cmd.Process.Pid, orderCpuStr)
	return nil
}

func (sb *Sealer) ExecPreCommit1(ctx context.Context, task WorkerTask) (storiface.PreCommit1Out, error) {
	args, err := json.Marshal(&ExecP1Req{
		Repo: sb.RepoPath(),
		Task: task,
	})
	if err != nil {
		return nil, errors.As(err)
	}

	cpuKeys, cpuVal,err := AllocateCpuForP1(ctx)
	if err != nil {
		return nil, errors.As(err)
	}
	defer bindcpu.ReturnCpus(cpuKeys)
	log.Infof("task: %+v,try bind cpu: %+v",task.SectorID,cpuVal.Cpus)
	// bind process
	if cpuVal.Conn == nil {
		if err := sb.bindP1Process(ctx, cpuKeys, cpuVal,task); err != nil {
			//bindcpu.CloseCpuConn(cpuKeys)
			return nil, errors.As(err)
		}
	}
	defer FreeTaskPid(task.SectorName())
	// write args
	encryptArg, err := AESEncrypt(args, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		//bindcpu.CloseCpuConn(cpuKeys)
		return nil, errors.As(err, string(args))
	}
	conn := cpuVal.Conn
	if _, err := conn.Write(encryptArg); err != nil {
		//bindcpu.CloseCpuConn(cpuKeys)
		return nil, errors.As(err, string(args))
	}

	// wait resp
	out, err := readUnixConn(conn)
	if err != nil {
		//bindcpu.CloseCpuConn(cpuKeys)
		return nil, errors.As(err, string(args))
	}
	decodeOut, err := AESDecrypt(out, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		return nil, errors.As(err, string(args))
	}
	resp := ExecP1Resp{}
	if err := json.Unmarshal(decodeOut, &resp); err != nil {
		return nil, errors.As(err, string(args), string(out))
	}
	if len(resp.Err) > 0 {
		return nil, errors.Parse(resp.Err).As(string(args), resp)
	}
	return resp.Data, nil
}

var P1Cmd = &cli.Command{
	Name:  "precommit1",
	Usage: "run precommit1 in process",
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

			resp := ExecP1Resp{}
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

			req := ExecP1Req{}
			if err := json.Unmarshal(argIn, &req); err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}

			workerRepo := req.Repo
			task := req.Task

			log.Infof("precommit1 process req in:%s, %s, %+v", unixAddr.String(), workerRepo, task.SectorID)
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
					SectorId:     storiface.SectorName(task.SectorID),
					SealedRepo:   workerRepo,
					UnsealedRepo: workerRepo,
					IsMarketSector: task.SectorStorage.UnsealedStorage.ID!=0,
				},
				StoreUnseal: task.StoreUnseal,
			}
			out, err := workerSealer.sealPreCommit1(ctx, sref, task.SealTicket, task.Pieces)
			if err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}
			resp.Data = out
			return
		}

		// handle connection
		for {
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
