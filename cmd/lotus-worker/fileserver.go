package main

import (
	"encoding/xml"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

type StorageFileServer struct {
	repo   string
	token  string
	router *mux.Router
}

type StorageDirectory struct {
	Href  string `xml:"href,attr"`
	Value string `xml:",chardata"`
}
type StorageDirectoryResp struct {
	XMLName xml.Name           `xml:"pre"`
	Files   []StorageDirectory `xml:"a"`
}

func NewStorageFileServer(repo, token string) *StorageFileServer {
	r := mux.NewRouter()
	r.PathPrefix("/storage/cache").Handler(http.StripPrefix("/storage/cache", http.FileServer(http.Dir(filepath.Join(repo, "cache")))))
	r.PathPrefix("/storage/unsealed").Handler(http.StripPrefix("/storage/unsealed", http.FileServer(http.Dir(filepath.Join(repo, "unsealed")))))
	r.PathPrefix("/storage/sealed").Handler(http.StripPrefix("/storage/sealed", http.FileServer(http.Dir(filepath.Join(repo, "sealed")))))

	r.HandleFunc("/storage/delete", func(w http.ResponseWriter, r *http.Request) {
		// TODO: make auth
		sid := r.FormValue("sid")
		if len(sid) == 0 {
			w.WriteHeader(404)
			w.Write([]byte("sector id not found"))
			return
		}
		if err := os.RemoveAll(filepath.Join(repo, "cache", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete cache failed:" + err.Error()))
			return
		}
		if err := os.RemoveAll(filepath.Join(repo, "sealed", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete sealed failed:" + err.Error()))
			return
		}
		if err := os.RemoveAll(filepath.Join(repo, "unsealed", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete unsealed failed:" + err.Error()))
			return
		}
	})

	return &StorageFileServer{repo: repo, token: token, router: r}
}

func (s *StorageFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: make auth
	s.router.ServeHTTP(w, r)
}
