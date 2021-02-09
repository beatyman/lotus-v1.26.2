package ffiwrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
)

type ExecPrecommit2Resp struct {
	Exit int
	Data storage.SectorCids
	Err  string
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
	cmd := exec.CommandContext(ctx, programName,
		"precommit2",
		"--worker-repo", repo,
		"--name", task.SectorName(),
	)
	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("NEPTUNE_DEFAULT_GPU=%s", gpuKey))

	var stdout bytes.Buffer
	// output the stderr log
	cmd.Stderr = os.Stderr
	cmd.Stdout = &stdout

	// write args to the program
	argIn, err := cmd.StdinPipe()
	if err != nil {
		return storage.SectorCids{}, errors.As(err)
	}

	if err := cmd.Start(); err != nil {
		return storage.SectorCids{}, errors.As(err)
	}

	// transfer precommit1 parameters
	if _, err := argIn.Write(args); err != nil {
		argIn.Close()
		return storage.SectorCids{}, errors.As(err, string(args))
	}
	argIn.Close() // write done

	// wait donDatae
	if err := cmd.Wait(); err != nil {
		return storage.SectorCids{}, errors.As(err, string(args))
	}

	resp := ExecPrecommit2Resp{}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return storage.SectorCids{}, errors.As(err, string(stdout.Bytes()))
	}
	if resp.Exit != 0 {
		return storage.SectorCids{}, errors.Parse(resp.Err)
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
	},
	Action: func(cctx *cli.Context) error {
		ctx, cancel := context.WithCancel(context.TODO())
		defer cancel()
		stdout := os.Stdout
		os.Stdout = os.Stderr

		resp := ExecPrecommit2Resp{
			Exit: 0,
		}
		defer func() {
			result, err := json.MarshalIndent(&resp, "", "	")
			if err != nil {
				os.Stderr.Write([]byte(err.Error()))
			} else {
				stdout.Write(result)
			}
		}()
		workerRepo, err := homedir.Expand(cctx.String("worker-repo"))
		if err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}

		input := ""
		if _, err := fmt.Scanln(&input); err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}
		task := WorkerTask{}
		if err := json.Unmarshal([]byte(input), &task); err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}

		workerSealer, err := New(RemoteCfg{}, &basicfs.Provider{
			Root: workerRepo,
		})
		if err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}
		out, err := workerSealer.SealPreCommit2(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, task.PreCommit1Out)
		if err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}
		resp.Data = out
		return nil
	},
}
