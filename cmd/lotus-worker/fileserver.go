package main

import (
	"net/http"
	"path/filepath"

	"github.com/gorilla/mux"
)

type StorageFileServer struct {
	repo  string
	token string
}

func NewStorageFileServer(repo, token string) *StorageFileServer {
	return &StorageFileServer{repo: repo, token: token}
}

func (s *StorageFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: make auth
	mux := mux.NewRouter()
	mux.PathPrefix("/storage/cache/").Handler(http.StripPrefix("/storage/cache/", http.FileServer(http.Dir(filepath.Join(s.repo, "cache")))))
	mux.PathPrefix("/storage/unsealed/").Handler(http.StripPrefix("/storage/unsealed/", http.FileServer(http.Dir(filepath.Join(s.repo, "unsealed")))))
	mux.PathPrefix("/storage/sealed/").Handler(http.StripPrefix("/storage/sealed/", http.FileServer(http.Dir(filepath.Join(s.repo, "seaeld")))))
	mux.ServeHTTP(w, r)
}
