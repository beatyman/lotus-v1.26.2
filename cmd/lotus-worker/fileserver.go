package main

import (
	"encoding/xml"
	"net/http"
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

	return &StorageFileServer{repo: repo, token: token, router: r}
}

func (s *StorageFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: make auth
	s.router.ServeHTTP(w, r)
}
