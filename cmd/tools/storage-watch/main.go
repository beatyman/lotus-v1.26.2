package main

import (
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

var (
	repoFlag = flag.String("storage-repos", "/data/zfs", "storage repo, seperate with ','")
	apiFlag  = flag.String("listen", ":1330", "api listen at")

	usernameFlag = flag.String("username", "hlm-miner", "http base auth")
	passwdFlag   = flag.String("passwd", "V4TitgRs0qJvWHwu", "http base auth")

	repos = []string{}
)

func main() {
	flag.Parse()

	repos = strings.Split(*repoFlag, ",")
	log.Infof("repo:%v", repos)

	go func() {
		for {
			log.Info("start protect")
			if err := protectPath(repos); err != nil {
				log.Warn(errors.As(err))
			}
			time.Sleep(1e9)
		}
	}()

	log.Infof("api:%s", *apiFlag)
	log.Fatal(http.ListenAndServe(*apiFlag, &HttpHandler{}))
}
