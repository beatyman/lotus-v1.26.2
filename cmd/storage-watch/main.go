package main

import (
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/filecoin-project/lotus/node/modules/auth"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

var (
	repoFlag = flag.String("storage-repos", "", "storage repo, seperate with ','")
	apiFlag  = flag.String("listen", ":1330", "api listen at")

	usernameFlag = flag.String("username", "hlm-miner", "http base auth")
	passwdFlag   = flag.String("passwd", "V4TitgRs0qJvWHwu", "http base auth")

	repos = []string{}
)

func main() {
	flag.Parse()
	if len(*repoFlag) > 0 {
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
	}

	log.Infof("api:%s", *apiFlag)
	crtPath := "./storage_crt.pem"
	keyPath := "./storage_key.pem"

	if err := auth.CreateTLSCert(crtPath, keyPath); err != nil {
		log.Fatal(errors.As(err))
		return
	}

	log.Fatal(http.ListenAndServeTLS(*apiFlag, crtPath, keyPath, &HttpHandler{}))
}
