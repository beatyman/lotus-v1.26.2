package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/client"
)

var downloadCmd = &cli.Command{
	Name:  "download",
	Usage: "[remote path] [local path]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "mode",
			Usage: "downlad mode, support mode: 'http', 'TODO:tcp'",
			Value: "http",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("arguments with [remote path] [local path]")
		}
		args := cctx.Args()
		if args.Len() != 2 {
			return fmt.Errorf("arguments with [remote path] [local path]")
		}
		remotePath := args.Get(0)
		localPath := args.Get(1)

		switch cctx.String("mode") {
		case "http":
			go func() {
				// TODO: process the download
				ctx := cctx.Context
				sid := remotePath
				ac := client.NewAuthClient(_authApiFlag, GetAuthData())
				newToken, err := ac.NewFileToken(ctx, sid)
				if err != nil {
					panic(err)
				}
				fc := client.NewHttpClient(_httpApiFlag, sid, string(newToken))
				if err := fc.Download(ctx, remotePath, localPath); err != nil {
					panic(err)
				}
			}()
		default:
			return fmt.Errorf("unknow mode '%s'", cctx.String("mode"))

		}
		// TODO: show the process
		end := make(chan os.Signal, 2)
		signal.Notify(end, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
		<-end
		return nil
	},
}
