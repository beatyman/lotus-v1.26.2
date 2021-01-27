package main

import (
	"context"
	"net/http"
	"os/exec"

	"github.com/gwaylib/errors"
)

func init() {
	RegisterHandle("/check", checkHandler)
}

func checkHandler(w http.ResponseWriter, r *http.Request) error {
	output, err := exec.CommandContext(context.TODO(), "zpool", "status", "-x").CombinedOutput()
	if err != nil {
		return errors.As(err)
	}
	return writeMsg(w, 200, string(output))
}
