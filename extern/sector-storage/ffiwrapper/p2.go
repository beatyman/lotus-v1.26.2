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
	args, err := json.Marshal(task)
	if err != nil {
		return storage.SectorCids{}, errors.As(err)
	}
	gpuKey, _, err := allocateGpu(ctx)
	if err != nil {
		log.Warn(errors.As(err))
	}
	defer returnGpu(gpuKey)

	programName := os.Args[0]
	unixAddr := filepath.Join(os.TempDir(), ".p2-"+uuid.New().String())
	defer os.Remove(unixAddr)

	cmd := exec.CommandContext(ctx, programName,
		"precommit2",
		"--worker-repo", repo,
		"--name", task.SectorName(),
		"--addr", unixAddr,
	)
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
		cmd.Process.Kill()
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
		out, err := workerSealer.SealPreCommit2(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, task.PreCommit1Out)
		if err != nil {
			resp.Err = errors.As(err, string(argIn)).Error()
			return nil
		}
		resp.Data = out
		return nil
	},
}
