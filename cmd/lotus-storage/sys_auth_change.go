package main

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/sys/auth/change", changeAuthHandler)
}

func changeAuthHandler(w http.ResponseWriter, r *http.Request) error {
	if !authBase(r) {
		return writeMsg(w, 401, "auth failed")
	}

	token := [16]byte(uuid.New())
	if err := ioutil.WriteFile(filepath.Join(_rootFlag, _authFile), token[:], 0600); err != nil {
		return errors.As(err)
	}

	log.Infof("changed auth success from:%s", r.RemoteAddr)

	if err := CleanPath(); err != nil {
		log.Error(errors.As(err))
	}

	_md5auth = fmt.Sprintf("%x", md5.Sum(token[:]))
	return writeMsg(w, 200, _md5auth)
}
