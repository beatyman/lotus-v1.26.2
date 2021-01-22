package main

import (
	"net/http"

	"github.com/gwaylib/errors"
)

type HandleFunc func(w http.ResponseWriter, r *http.Request) error

var handles = map[string]HandleFunc{}

func RegisterHandle(path string, handle HandleFunc) {
	_, ok := handles[path]
	if ok {
		panic("already registered:" + path)
	}
	handles[path] = handle
}

type HttpHandler struct {
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handle, ok := handles[r.URL.Path]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte("Not found"))
		return
	}

	// auth
	username, passwd, ok := r.BasicAuth()
	if !ok {
		w.WriteHeader(401)
		w.Write([]byte("auth failed"))
		return
	}
	if username != *usernameFlag || passwd != *passwdFlag {
		w.WriteHeader(401)
		w.Write([]byte("auth failed"))
		return
	}

	if err := handle(w, r); err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
}

func writeMsg(w http.ResponseWriter, code int, msg string) error {
	w.WriteHeader(code)
	if _, err := w.Write([]byte(msg)); err != nil {
		return errors.As(err)
	}
	return nil
}
