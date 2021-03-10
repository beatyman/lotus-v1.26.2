package impl

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/gwaylib/errors"
)

type ZpoolQuery struct {
	out        []byte
	createTime time.Time
}

var (
	zpoolCache *ZpoolQuery
	zpoolLk    sync.Mutex
)

func (sm *StorageMinerAPI) StatusMinerStorage(ctx context.Context) ([]byte, error) {
	zpoolLk.Lock()
	defer zpoolLk.Unlock()
	now := time.Now()
	if zpoolCache != nil && now.Sub(zpoolCache.createTime) < 5*time.Minute {
		return zpoolCache.out, nil
	}

	_, err := exec.LookPath("zpool")
	if err != nil {
		out := []byte("zpool command not found\n")
		zpoolCache = &ZpoolQuery{
			out:        out,
			createTime: now,
		}
		return out, nil
	}

	out, err := exec.CommandContext(ctx, "zpool", "status", "-x").CombinedOutput()
	if err != nil {
		return nil, errors.As(err)
	}
	zpoolCache = &ZpoolQuery{
		out:        out,
		createTime: now,
	}
	return zpoolCache.out, nil
}
