package main

import (
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestFileServer(t *testing.T) {
	// simulating static file
	sealedRepo := "./fileserver"
	fileServerToken := "testing"
	if err := os.MkdirAll(sealedRepo+"/cache/t0100", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sealedRepo+"/unsealed", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sealedRepo+"/sealed", 0755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(sealedRepo)
	}()
	if err := ioutil.WriteFile(sealedRepo+"/cache/t0100/1", []byte("testing"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(sealedRepo+"/cache/t0100/2", []byte("testing"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(sealedRepo+"/unsealed/t0100", []byte("testing"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(sealedRepo+"/sealed/t0100", []byte("testing"), 0666); err != nil {
		t.Fatal(err)
	}

	fileServer := ":1280"
	fileHandle := NewStorageFileServer(sealedRepo, string(fileServerToken))
	go func() {
		log.Info("File server listen at: " + fileServer)
		if err := http.ListenAndServe(fileServer, fileHandle); err != nil {
			panic(err)
		}
	}()
	time.Sleep(1e9)

	resp, err := http.Get("http://127.0.0.1:1280/storage/cache/t0100/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r := &StorageDirectoryResp{}
	if err := xml.Unmarshal(respData, r); err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 2 {
		t.Fatal("expect 2 files,but:", len(r.Files))
	}
}
