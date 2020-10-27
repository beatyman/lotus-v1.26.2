package main

import (
	"context"
	"encoding/json"
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

func ExecPrecommit1(ctx context.Context, ssize abi.SectorSize, task ffiwrapper.WorkerTask) (storage.PreCommit1Out, error) {
	args, err := json.Marshal(task)
	if err != nil {
		return nil, errors.As(err)
	}
	var cpuKey = 0
	var cpuGroup = []int{}
	for {
		idx, group, err := allocateCpu(ctx)
		if err != nil {
			log.Warn(errors.As(err))
			time.Sleep(10e9)
			continue
		}
		cpuKey = idx
		cpuGroup = group
		break
	}
	defer returnCpu(cpuKey)

	programName := os.Args[0]
	cmd := exec.CommandContext(ctx, programName, "precommit", "ssize", strconv.FormatUint(uint64(ssize), 10))
	// output the stderr log
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, errors.As(err)
	}

	// set cpu affinity
	cpuSet := unix.CPUSet{}
	for _, cpu := range cpuGroup {
		cpuSet.Set(cpu)
	}
	if err := unix.SchedSetaffinity(cmd.Process.Pid, &cpuSet); err != nil {
		log.Error(errors.As(err))
	}

	// write args to the program
	argIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.As(err)
	}
	if _, err := argIn.Write(args); err != nil {
		argIn.Close()
		return nil, errors.As(err)
	}
	argIn.Close() // write done

	// wait done
	if err := cmd.Wait(); err != nil {
		return nil, errors.As(err)
	}

	// return result
	out, err := cmd.Output()
	return storage.PreCommit1Out(out), err
}
