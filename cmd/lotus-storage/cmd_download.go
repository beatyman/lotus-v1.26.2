package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/client"
)

var downloadCmd = &cli.Command{
	Name:  "download",
	Usage: "[remote path] [local path]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "mode",
			Usage: "downlad mode, support mode: 'http', 'tcp'. http mode support directory and resume download, tcp only support one file for debug fuse function.",
			Value: "http",
		},
		&cli.IntFlag{
			Name:  "read-size",
			Usage: "buffer size for read",
			Value: 1024 * 1024,
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

		end := make(chan os.Signal, 2)
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
				log.Infof("start download: %s->%s", remotePath, localPath)
				startTime := time.Now()
				fc := client.NewHttpClient(_httpApiFlag, sid, string(newToken))
				if err := fc.Download(ctx, localPath, remotePath); err != nil {
					panic(err)
				}
				log.Infof("end download: %s->%s, took:%s", remotePath, localPath, time.Now().Sub(startTime))
				end <- os.Kill
			}()
		default:
			return fmt.Errorf("unknow mode '%s'", cctx.String("mode"))

		}
		// TODO: show the process
		signal.Notify(end, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
		<-end
		return nil
	},
}
