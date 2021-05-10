package main

import (
	"net/http"
	"path/filepath"
	"strings"
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
	if !_handler.VerifyToken(username, passwd) {
		log.Infof("auth failed:%s,%s,%s", r.RemoteAddr, username, passwd)
		return "", false
	}
	return "", authRW(username, passwd, file)
}

func authRW(user, auth, path string) bool {
	if !_handler.VerifyToken(user, auth) {
		return false
	}
	if !strings.Contains(path, user) {
		return false
	}
	return true
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
