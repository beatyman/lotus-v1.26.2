package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/gwaylib/errors"
)

var mountCmd = &cli.Command{
	Name:  "mount",
	Usage: "[mountpoint]",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("mountpoint not found")
		}
		mountPoint := cctx.Args().First()

		authData := GetAuthRO()
		fuc := client.NewFUseClient(_posixFsApiFlag, authData)
		if err := fuc.Mount(cctx.Context, mountPoint); err != nil {
			return errors.As(err)
		}
		end := make(chan os.Signal, 2)
		signal.Notify(end, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
		<-end
		return nil
	},
}
