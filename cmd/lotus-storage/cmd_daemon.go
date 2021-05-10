package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/filecoin-project/lotus/node/modules/auth"
	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"
)

var daemonCmd = &cli.Command{
	Name:  "daemon",
	Usage: "Start a daemon to run storage server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "storage-repo",
			Usage: "storage data repo",
			Value: "/data/zfs",
		},
	},
	Action: func(cctx *cli.Context) error {
		_repoFlag = cctx.String("storage-repo")

		if err := InitDB(_rootFlag); err != nil {
			return errors.As(err)
		}

		// stop the chattr protect.
		// TODO: remove this code
		repo := _repoFlag
		if err := ioutil.WriteFile(filepath.Join(repo, "miner-check.dat"), []byte("success"), 0600); err != nil {
			log.Fatal(errors.As(err, repo))
		}
		if err := os.MkdirAll(filepath.Join(repo, "cache"), 0755); err != nil {
			log.Fatal(errors.As(err, repo))
		}
		if err := os.MkdirAll(filepath.Join(repo, "sealed"), 0755); err != nil {
			log.Fatal(errors.As(err, repo))
		}
		if err := os.MkdirAll(filepath.Join(repo, "unsealed"), 0755); err != nil {
			log.Fatal(errors.As(err, repo))
		}

		// for read export
		go func() {
			log.Infof("pxfs-api:%s", _posixFsApiFlag)
			log.Fatal(FUseFileServer(_posixFsApiFlag))

			// using nfs for posix file, but there are bug in concurency.
			//listener, err := net.Listen("tcp", *_signalApiFlag)
			//if err != nil {
			//	panic(err)
			//}
			//handler := NewNFSAuthHandler()
			//log.Fatal(nfs.Serve(listener, handler))
		}()

		// for http download and upload
		go func() {
			log.Infof("http-api:%s", _httpApiFlag)
			log.Fatal(http.ListenAndServe(_httpApiFlag, _handler))
		}()

		// for https auth command
		crtPath := filepath.Join(_rootFlag, "storage_crt.pem")
		keyPath := filepath.Join(_rootFlag, "storage_key.pem")
		if err := auth.CreateTLSCert(crtPath, keyPath); err != nil {
			return errors.As(err)
		}
		log.Infof("auth-api:%s", _authApiFlag)
		return http.ListenAndServeTLS(_authApiFlag, crtPath, keyPath, _handler)

	},
}
