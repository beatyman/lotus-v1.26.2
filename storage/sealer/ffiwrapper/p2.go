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

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
)

type ExecPrecommit2Resp struct {
	Data storiface.SectorCids
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

func ExecPrecommit2WithSupra(ctx context.Context, sealer *Sealer, task WorkerTask) (storiface.SectorCids, error) {
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecPrecommit2WithSupra panic: %v\n%v", e, string(debug.Stack()))
		}
	}()

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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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
	commR, err := commcid.DataCommitmentV1ToCID(commRBytes)
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

func ExecPrecommit2(ctx context.Context, repo string, task WorkerTask) (storiface.SectorCids, error) {
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecPrecommit2 panic: %v\n%v", e, string(debug.Stack()))
		}
	}()

	AssertGPU(ctx)

	args, err := json.Marshal(task)
	if err != nil {
		return storiface.SectorCids{}, errors.As(err)
	}
	gpuKey, _, err := allocateGpu(ctx)
	if err != nil {
		log.Errorw("allocate gpu error", "task-key", task.Key(), "err", errors.As(err))
		//return storage.SectorCids{}, errors.As(err)
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
		return storiface.SectorCids{}, errors.As(err, string(args))
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
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	defer conn.Close()

	log.Infof("ExecPrecommit2 dial unix success: worker(%v) sector(%v)", task.WorkerID, task.SectorID)
	if _, err := conn.Write(args); err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	// wait donDatae
	out, err := readUnixConn(conn)
	if err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}

	resp := ExecPrecommit2Resp{}
	if err := json.Unmarshal(out, &resp); err != nil {
		return storiface.SectorCids{}, errors.As(err, string(args))
	}
	if len(resp.Err) > 0 {
		return storiface.SectorCids{}, errors.Parse(resp.Err).As(args)
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
		defer func() {
			result, err := json.Marshal(&resp)
			if err != nil {
				panic(err)
			}
			if _, err := conn.Write(result); err != nil {
				panic(err)
			}
		}()

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
		out, err := workerSealer.SealPreCommit2(ctx, storiface.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, task.PreCommit1Out)
		if err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
			return nil
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
