package main

import (
	"context"
	"net/http"
	"os/exec"
	"time"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/check", checkHandler)
}

func checkHandler(w http.ResponseWriter, r *http.Request) error {
	_handler.checkCacheLk.Lock()
	defer _handler.checkCacheLk.Unlock()
	now := time.Now()
	if _handler.checkCache != nil && now.Sub(_handler.checkCache.createTime) < time.Minute {
		return writeMsg(w, 200, _handler.checkCache.out)
	}

	output, err := exec.CommandContext(context.TODO(), "zpool", "status", "-x").CombinedOutput()
	if err != nil {
		return errors.As(err)
	}
	_handler.checkCache = &CheckCache{
		out:        string(output),
		createTime: now,
	}
	return writeMsg(w, 200, string(output))
}
