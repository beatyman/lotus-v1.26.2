package ffiwrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
)

type ExecPrecommit2Resp struct {
	Data storage.SectorCids
	Err  string
}

func readUnixConn(conn net.Conn) ([]byte, error) {
	result := []byte{}
	bufLen := 32 * 1024
	buf := make([]byte, bufLen)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Error(err)
			if err != io.EOF {
				return nil, errors.As(err)
			}
		}
		result = append(result, buf[:n]...)
		if n < bufLen {
			break
		}
	}
	return result, nil
}

func ExecPrecommit2(ctx context.Context, repo string, task WorkerTask) (storage.SectorCids, error) {
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecPrecommit2 panic: %v\n%v", e, string(debug.Stack()))
		}
	}()

	AssertGPU(ctx)

	args, err := json.Marshal(task)
	if err != nil {
		return storage.SectorCids{}, errors.As(err)
	}
	gpuKey, _, err := allocateGpu(ctx)
	if err != nil {
		log.Errorw("allocate gpu error", "task-key", task.Key(), "err", errors.As(err))
		return storage.SectorCids{}, errors.As(err)
	}
	defer returnGpu(gpuKey)

	programName := os.Args[0]

	if task.SectorRepairStatus == 2 {
		programName = "./lotus-worker-repair"
	}

	unixAddr := filepath.Join(os.TempDir(), ".p2-"+uuid.New().String())
	defer os.Remove(unixAddr)

	var cmd *exec.Cmd
	if os.Getenv("LOTUS_WORKER_P2_CPU_LIST") != "" {
		cpuList := os.Getenv("LOTUS_WORKER_P2_CPU_LIST")
		log.Infof("Lock P2 cpu list: %v ", cpuList)
		cmd = exec.CommandContext(ctx, "taskset", "-c", cpuList, programName,
			"precommit2",
			"--worker-repo", repo,
			"--name", task.SectorName(),
			"--addr", unixAddr,
		)
	} else {
		cmd = exec.CommandContext(ctx, programName,
			"precommit2",
			"--worker-repo", repo,
			"--name", task.SectorName(),
			"--addr", unixAddr,
		)
	}
	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU=%s", gpuKey))
	// output the stderr log
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return storage.SectorCids{}, errors.As(err, string(args))
	}
	defer func() {
		time.Sleep(3e9)    // wait 3 seconds for exit.
		cmd.Process.Kill() // TODO: restfull exit.
		if err := cmd.Wait(); err != nil {
			log.Error(err)
		}
	}()
	// transfer precommit1 parameters
	var d net.Dialer
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	d.LocalAddr = nil // if you have a local addr, add it here
	raddr := net.UnixAddr{Name: unixAddr, Net: "unix"}
	retryTime := 0

loopUnixConn:
	conn, err := d.DialContext(ctx, "unix", raddr.String())
	if err != nil {
		retryTime++
		if retryTime < 10 {
			time.Sleep(1e9)
			goto loopUnixConn
		}
		return storage.SectorCids{}, errors.As(err, string(args))
	}
	defer conn.Close()

	log.Infof("ExecPrecommit2 dial unix success: worker(%v) sector(%v)", task.WorkerID, task.SectorID)
	if _, err := conn.Write(args); err != nil {
		return storage.SectorCids{}, errors.As(err, string(args))
	}
	// wait donDatae
	out, err := readUnixConn(conn)
	if err != nil {
		return storage.SectorCids{}, errors.As(err, string(args))
	}

	resp := ExecPrecommit2Resp{}
	if err := json.Unmarshal(out, &resp); err != nil {
		return storage.SectorCids{}, errors.As(err, string(args))
	}
	if len(resp.Err) > 0 {
		return storage.SectorCids{}, errors.Parse(resp.Err).As(args)
	}
	return resp.Data, nil
}

var P2Cmd = &cli.Command{
	Name:  "precommit2",
	Usage: "run precommit2 in process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "worker-repo",
			EnvVars: []string{"LOTUS_WORKER_PATH", "WORKER_PATH"},
			Value:   "~/.lotusworker", // TODO: Consider XDG_DATA_HOME
		},
		&cli.StringFlag{
			Name: "name", // just for process debug
		},
		&cli.StringFlag{
			Name: "addr", // listen address
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, cancel := context.WithCancel(context.TODO())
		defer cancel()

		unixAddr := net.UnixAddr{Name: cctx.String("addr"), Net: "unix"}
		// unix listen
		ln, err := net.ListenUnix("unix", &unixAddr)
		if err != nil {
			panic(err)
		}
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		defer conn.Close()
		argIn, err := readUnixConn(conn)
		if err != nil {
			panic(err)
		}
		resp := ExecPrecommit2Resp{}

		workerRepo, err := homedir.Expand(cctx.String("worker-repo"))
		if err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
			return nil
		}
		task := WorkerTask{}
		if err := json.Unmarshal(argIn, &task); err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
			return nil
		}
		workerSealer, err := New(RemoteCfg{}, &basicfs.Provider{
			Root: workerRepo,
		})
		if err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
			return nil
		}
		out, err := workerSealer.SealPreCommit2(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, task.PreCommit1Out)
		if err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
		}
		resp.Data = out
		log.Infof("SealPreCommit2: %+v ", resp)
		result, err := json.Marshal(&resp)
		if err != nil {
			log.Error(err)
		}
		if _, err := conn.Write(result); err != nil {
			log.Error(err)
		}
		ch := make(chan int)
		select {
		case <-ch:
		case <-time.After(time.Second * 30):
			log.Info("SealPreCommit2 timeout 30s")
		}
		log.Info("SealPreCommit2 Write Success")
		return nil
	},
}
