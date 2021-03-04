package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"
)

var (
	usernameFlag = flag.String("username", "hlm-miner", "http base auth")
	passwdFlag   = flag.String("passwd", "V4TitgRs0qJvWHwu", "http base auth")
)

func main() {
	flag.Parse()

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

		path := "./node"
		dirs, err := ioutil.ReadDir(path)
		if err != nil {
			io.WriteString(w, err.Error())
			return
		}
		for _, fs := range dirs {
			// ignore the chain
			if fs.Name() == "lotus" {
				continue
			}
			filePath := filepath.Join(path, fs.Name(), ".lotusstorage")
			if !fs.IsDir() {
				continue
			}
			ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
			defer cancel()
			io.WriteString(w, fmt.Sprintf("\n\n==============%s begin==============\n", fs.Name()))
			r0, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", filePath, "proving", "deadlines").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				continue
			}
			w.Write(r0)

			r2, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", filePath, "info").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				continue
			}
			w.Write(r2)
			r1, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", filePath, "proxy", "status", "--mpool").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				continue
			}
			w.Write(r1)
			r3, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", filePath, "actor", "control", "list").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				continue
			}
			w.Write(r3)
			r4, err := exec.CommandContext(ctx, "./lotus-miner", "--repo", "./node/lotus", "--miner-repo", filePath, "hlm-storage", "status").CombinedOutput()
			if err != nil {
				io.WriteString(w, err.Error()+"\n")
				continue
			}
			w.Write(r4)
			io.WriteString(w, fmt.Sprintf("==============%s end==============\n", fs.Name()))
		}
	})

	// One can use generate_cert.go in crypto/tls to generate cert.pem and key.pem.
	log.Printf("About to listen on 8443. Go to https://127.0.0.1:8443/")
	err := http.ListenAndServeTLS(":8443", "lotus_crt.pem", "lotus_key.pem", nil)
	log.Fatal(err)
}
