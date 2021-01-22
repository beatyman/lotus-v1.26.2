package main

import (
	"net/http"
)

func init() {
	RegisterHandle("/check", checkHandler)
}

func checkHandler(w http.ResponseWriter, r *http.Request) error {
	return writeMsg(w, 200, "1")
}
