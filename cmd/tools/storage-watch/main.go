package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
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
	fmt.Printf("repo:%s\n", *repoFlag)
	fmt.Printf("api:%s\n", *apiFlag)

	repos = strings.Split(*repoFlag, ",")

	log.Fatal(http.ListenAndServe(*apiFlag, &HttpHandler{}))
}
