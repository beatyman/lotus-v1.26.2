// command
// list nodes
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/nodes
//
// ./miner.sh proving deadlines
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/monitor?node=node-1&kind=wdpost
//
// ./miner.sh info
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/monitor?node=node-1&kind=miner
//
// ./miner.sh proxy status --sync --mpool
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/monitor?node=node-1&kind=proxy
//
// ./miner.sh actor control list
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/monitor?node=node-1&kind=balance
//
// ./miner.sh hlm-storage status
// curl -k -u hlm-miner:V4TitgRs0qJvWHwu https://127.0.0.1:8443/monitor?node=node-1&kind=hlm-storage
package main

import (
	"context"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/filecoin-project/lotus/node/modules/auth"
)

var (
	addrFlag     = flag.String("addr", "127.0.0.1:8443", "listen address")
	usernameFlag = flag.String("username", "hlm-miner", "http base auth")
	passwdFlag   = flag.String("passwd", "V4TitgRs0qJvWHwu", "http base auth")
)

func main() {
	flag.Parse()
	http.HandleFunc("/nodes", func(w http.ResponseWriter, req *http.Request) {
		username, passwd, ok := req.BasicAuth()
		if !ok {
			io.WriteString(w, "auth failed!\n")
			return
		}
		if username != *usernameFlag || passwd != *passwdFlag {
			io.WriteString(w, "auth failed, bye!\n")
			return
		}

		dirs, err := ioutil.ReadDir("./node")
		if err != nil {
			io.WriteString(w, err.Error()+"\n")
			return
		}
		for _, dir := range dirs {
			io.WriteString(w, dir.Name()+"\n")
		}
	})

	http.HandleFunc("/monitor", func(w http.ResponseWriter, req *http.Request) {
		username, passwd, ok := req.BasicAuth()
		if !ok {
			io.WriteString(w, "auth failed!\n")
			return
		}
		if username != *usernameFlag || passwd != *passwdFlag {
			io.WriteString(w, "auth failed, bye!\n")
			return
		}
		node := req.FormValue("node")
		if len(node) == 0 {
			io.WriteString(w, "no node!\n")
			return
		}
		kind := req.FormValue("kind")
		if len(kind) == 0 {
			io.WriteString(w, "no kind!\n")
			return
		}

		path := "./node"
		repo := filepath.Join(path, node, ".lotusstorage")
		ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
		defer cancel()
		switch kind {
		case "wdpost":
			r0, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", repo, "proving", "deadlines").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				return
			}
			w.Write(r0)

		case "miner":
			info, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", repo, "info").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				return
			}
			w.Write(info)
		case "proxy":
			proxy, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", repo, "proxy", "status", "--sync", "--mpool").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				return
			}
			w.Write(proxy)
		case "balance":
			r3, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", repo, "actor", "control", "list").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				return
			}
			w.Write(r3)
		case "hlm-storage":
			r4, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", repo, "hlm-storage", "status").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				return
			}
			w.Write(r4)
		default:
			io.WriteString(w, "unknow kind\n")
			return
		}
	})

	if err := auth.CreateTLSCert("./lotus_crt.pem", "./lotus_key.pem"); err != nil {
		log.Fatal(err)
	}
	// One can use generate_cert.go in crypto/tls to generate cert.pem and key.pem.
	log.Printf("About to listen on %s", *addrFlag)
	err := http.ListenAndServe(*addrFlag,  nil)
	log.Fatal(err)
}
