package main

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gwaylib/log"
)

func validHttpFilePath(file string) bool {
	repo := _repoFlag
	tPath, err := filepath.Abs(filepath.Join(repo, file))
	if err != nil {
		return false
	}
	if !strings.HasPrefix(tPath, repo) {
		return false
	}
	return true
}
func authFile(r *http.Request, write bool) (string, bool) {
	username, passwd, ok := r.BasicAuth()
	if !ok {
		log.Infof("no BasicAuth:%s", r.RemoteAddr)
		return "", false
	}
	file := r.FormValue("file")
	if !validHttpFilePath(file) {
		return "", false
	}
	switch username {
	case "sys":
		if write || passwd != fmt.Sprintf("%x", md5.Sum([]byte(_md5auth+"read"))) {
			return "", false
		}
		return file, true
	default:
		if !_handler.VerifyToken(username, passwd) {
			log.Infof("auth failed:%s,%s,%s", r.RemoteAddr, username, passwd)
			return "", false
		}
		if write && !strings.Contains(file, username) {
			return "", false
		}
		return file, true
	}
	return "", false
}

func authBase(r *http.Request) bool {
	// auth
	username, passwd, ok := r.BasicAuth()
	if !ok {
		// TODO: limit the failed
		log.Infof("auth failed:%s,%s,%s", r.RemoteAddr, username)
		return false
	}
	return passwd == _md5auth
}
