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

var uploadCmd = &cli.Command{
	Name:  "upload",
	Usage: "[local path] [remote path]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "mode",
			Usage: "downlad mode, support mode: 'http', 'TODO:tcp'",
			Value: "http",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("arguments with [local path] [remote path]")
		}
		args := cctx.Args()
		if args.Len() != 2 {
			return fmt.Errorf("arguments with [local path] [remote path]")
		}
		localPath := args.Get(0)
		remotePath := args.Get(1)

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
				log.Infof("start upload: %s->%s", localPath, remotePath)
				startTime := time.Now()
				fc := client.NewHttpClient(_httpApiFlag, sid, string(newToken))
				if err := fc.Upload(ctx, localPath, remotePath); err != nil {
					panic(err)
				}
				log.Infof("end upload: %s->%s, took:%s", localPath, remotePath, time.Now().Sub(startTime))
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
