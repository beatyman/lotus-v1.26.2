package main

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/build"
)

func BuildVersion() string {
	return build.BuildVersion + build.CurrentCommit
}

const (
	_authFile = "auth.dat"
	_dbFile   = "datastore.db"
)

var (
	// common flag
	_rootFlag    = ""
	_authApiFlag = ""
	_httpApiFlag = ""

	_md5auth = ""

	// flag for server
	_repoFlag = ""
	_handler  = &HttpHandler{
		token: map[string]Token{},
	}
)

func GetAuthData() string {
	return _md5auth
}
func GetAuthRO() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"read")))
}

func GetAuthRW() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"write")))
}

func main() {
	commands := []*cli.Command{
		daemonCmd,
		downloadCmd,
		uploadCmd,
		//authCmd,
	}
	app := &cli.App{
		Name:    "lotus-storage",
		Usage:   "storage server to replace nfs for lotus",
		Version: BuildVersion(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "storage-root",
				Value: "", // default at current directory.
				Usage: "storage work home",
			},
			&cli.StringFlag{
				Name:  "addr-auth",
				Usage: "auth api listen at",
				Value: "127.0.0.1:1330",
			},
			&cli.StringFlag{
				Name:  "addr-http",
				Usage: "http transfer api listen at",
				Value: "127.0.0.1:1331",
			},
			&cli.StringFlag{
				Name:  "addr-pxfs",
				Usage: "TODO: remove this",
				Value: "127.0.0.1:1332",
			},
		},
		Commands: commands,
		Before: func(cctx *cli.Context) error {
			_rootFlag = cctx.String("storage-root")
			_authApiFlag = cctx.String("addr-auth")
			_httpApiFlag = cctx.String("addr-http")
			authFile := filepath.Join(_rootFlag, _authFile)
			if err := os.MkdirAll(filepath.Dir(authFile), 0755); err != nil {
				log.Fatal(errors.As(err))
			}
			authData, err := ioutil.ReadFile(authFile)
			if err != nil {
				if !os.IsNotExist(err) {
					return errors.As(err)
				}
				// using the empty auth
			}
			_md5auth = fmt.Sprintf("%x", md5.Sum(authData)) // empty of md5 is 'd41d8cd98f00b204e9800998ecf8427e'
			return nil
		},
	}
	app.Setup()
	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}
