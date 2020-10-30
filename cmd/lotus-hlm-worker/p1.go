package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"golang.org/x/sys/unix"
)

func ExecPrecommit1(ctx context.Context, repo string, ssize abi.SectorSize, task ffiwrapper.WorkerTask) (storage.PreCommit1Out, error) {
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
	cmd := exec.CommandContext(ctx, programName, "--worker-repo", repo, "precommit1", "--ssize", strconv.FormatUint(uint64(ssize), 10))
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

	// return result
	return storage.PreCommit1Out(stdout.Bytes()), nil
}
