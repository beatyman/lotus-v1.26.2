package ffiwrapper

import (
	"context"
	"os"
	"strconv"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/hardware/bindgpu"
)

func BindGPU(ctx context.Context) {
	defaultGPUEnv := os.Getenv("NEPTUNE_DEFAULT_GPU")
	if len(defaultGPUEnv) > 0 {
		// alreay set
		return
	}

	envIdxStr := os.Getenv("NEPTUNE_DEFAULT_GPU_IDX")
	if len(envIdxStr) == 0 {
		// GPU_IDX not set, using default
		return
	}

	envIdx, err := strconv.Atoi(envIdxStr)
	if err != nil {
		log.Warn(errors.As(err, envIdxStr))
		return
	}
	bindgpu.BindGPU(ctx, envIdx)
	log.Infof("DEBUG: bind gpu, %d", envIdx)
}
