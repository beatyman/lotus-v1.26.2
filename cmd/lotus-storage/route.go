package main

import (
	"encoding/json"
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

func writeMsg(w http.ResponseWriter, code int, msg string) error {
	w.WriteHeader(code)
	if _, err := w.Write([]byte(msg)); err != nil {
		return errors.As(err)
	}
	return nil
}
func writeJson(w http.ResponseWriter, code int, obj interface{}) error {
	output, err := json.MarshalIndent(obj, "", "	")
	if err != nil {
		return errors.As(err)
	}

	w.WriteHeader(code)
	if _, err := w.Write(output); err != nil {
		return errors.As(err)
	}
	return nil
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//log.Infof("from:%s,method:%s,path:%s", r.RemoteAddr, r.Method, r.URL.String())
	handle, ok := handles[r.URL.Path]
	if !ok {
		writeMsg(w, 404, "Not found")
		return
	}

	if err := handle(w, r); err != nil {
		writeMsg(w, 500, err.Error())
		return
	}
	return
}
