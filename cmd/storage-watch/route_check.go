package main

import (
	"context"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/check", checkHandler)
}

type CheckCache struct {
	out        string
	createTime time.Time
}

var checkCache *CheckCache
var checkCacheLk sync.Mutex

func checkHandler(w http.ResponseWriter, r *http.Request) error {
	checkCacheLk.Lock()
	defer checkCacheLk.Unlock()
	now := time.Now()
	if checkCache != nil && now.Sub(checkCache.createTime) < 5*time.Minute {
		return writeMsg(w, 200, checkCache.out)
	}

	output, err := exec.CommandContext(context.TODO(), "zpool", "status", "-x").CombinedOutput()
	if err != nil {
		return errors.As(err)
	}
	checkCache = &CheckCache{
		out:        string(output),
		createTime: now,
	}
	return writeMsg(w, 200, string(output))
}
