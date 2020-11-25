package ffiwrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/sys/unix"
)

type ExecPrecommit1Resp struct {
	Exit int
	Data []byte
	Err  string
}

func ExecPrecommit1(ctx context.Context, repo string, task WorkerTask) (storage.PreCommit1Out, error) {
	args, err := json.Marshal(task)
	if err != nil {
		return nil, errors.As(err)
	}
	var cpuIdx = 0
	var cpuGroup = []int{}
	for {
		idx, group, err := allocateCpu(ctx)
		if err != nil {
			log.Warn(errors.As(err))
			time.Sleep(10e9)
			continue
		}
		cpuIdx = idx
		cpuGroup = group
		break
	}
	defer returnCpu(cpuIdx)

	programName := os.Args[0]
	cmd := exec.CommandContext(ctx, programName, "--worker-repo", repo,
		"precommit1",
		"--name", task.SectorStorage.SectorInfo.ID,
	)
	// set the env
	cmd.Env = os.Environ()
	if len(cpuGroup) < 2 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_MULTICORE_SDR_PRODUCERS=1"))
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_USE_MULTICORE_SDR=0"))
	} else {
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_MULTICORE_SDR_PRODUCERS=%d", len(cpuGroup)))
		cmd.Env = append(cmd.Env, fmt.Sprintf("FIL_PROOFS_USE_MULTICORE_SDR=1"))
	}

	var stdout bytes.Buffer
	defer func() {
		fmt.Println(cmd.String())
		fmt.Println(string(args))
	}()
	// output the stderr log
	cmd.Stderr = os.Stderr
	cmd.Stdout = &stdout

	// write args to the program
	argIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.As(err)
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.As(err)
	}

	// set cpu affinity
	cpuSet := unix.CPUSet{}
	for _, cpu := range cpuGroup {
		cpuSet.Set(cpu)
	}
	// https://github.com/golang/go/issues/11243
	if err := unix.SchedSetaffinity(cmd.Process.Pid, &cpuSet); err != nil {
		log.Error(errors.As(err))
	}

	// transfer precommit1 parameters
	if _, err := argIn.Write(args); err != nil {
		argIn.Close()
		return nil, errors.As(err, string(args))
	}
	argIn.Close() // write done

	// wait done
	if err := cmd.Wait(); err != nil {
		return nil, errors.As(err, string(args))
	}

	resp := ExecPrecommit1Resp{}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, errors.As(err, string(stdout.Bytes()))
	}
	if resp.Exit != 0 {
		return nil, errors.Parse(resp.Err)
	}
	return storage.PreCommit1Out(resp.Data), nil
}

var P1Cmd = &cli.Command{
	Name:  "precommit1",
	Usage: "run precommit1 in process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "name", // just for process debug
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, cancel := context.WithCancel(context.TODO())
		defer cancel()

		resp := ExecPrecommit1Resp{
			Exit: 0,
		}
		defer func() {
			result, err := json.MarshalIndent(&resp, "", "	")
			if err != nil {
				os.Stderr.Write([]byte(err.Error()))
			} else {
				os.Stdout.Write(result)
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
		pieceInfo, err := DecodePieceInfo(task.Pieces)
		if err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}
		rspco, err := workerSealer.SealPreCommit1(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, task.SealTicket, pieceInfo)
		if err != nil {
			resp.Exit = 1
			resp.Err = errors.As(err).Error()
			return nil
		}
		resp.Data = rspco
		return nil
	},
}
