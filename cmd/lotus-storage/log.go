package main

import (
	"os"

	"github.com/gwaylib/log/logger"
	"github.com/gwaylib/log/logger/adapter/stdio"
	"github.com/gwaylib/log/proto"
)

var log *logger.Logger

func init() {
	level := proto.LevelInfo
	if os.Getenv("LOTUS_FUSE_DEBUG") == "1" {
		level = proto.LevelDebug
	}
	log = logger.New(&proto.Context{"lotus-storage", "1.0.0", logger.HostName}, "server", level, stdio.New(os.Stdout, os.Stderr))

}
