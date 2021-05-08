package client

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestHttpClient(t *testing.T) {
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("./test")

	fc := NewHttpClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}
	if _, err := fc.FileStat(ctx, filepath.Join("sealed", sid)); err != nil {
		t.Fatal(err)
	}

	localpath := "/usr/local/go/bin/go"
	localStat, err := os.Stat(localpath)
	if err != nil {
		t.Fatal(err)
	}
	n, err := fc.upload(ctx, localpath, filepath.Join("sealed", sid), false)
	if err != nil {
		t.Fatal(err)
	}
	if n != localStat.Size() {
		t.Fatalf("expect %d==%d", n, localStat.Size())
	}
	n, err = fc.upload(ctx, "/usr/local/go/bin/go", filepath.Join("sealed", sid), true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("already uploaded, so it is expected 0, but:%d", n)
	}

	n, err = fc.download(ctx, "./test", filepath.Join("sealed", sid))
	if err != nil {
		t.Fatal(err)
	}
	newStat, err := os.Stat("./test")
	if err != nil {
		t.Fatal(err)
	}
	if newStat.Size() != localStat.Size() {
		t.Fatalf("upload and download not match %d:%d", newStat.Size(), localStat.Size())
	}
	n, err = fc.download(ctx, "./test", filepath.Join("sealed", sid))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("already downloaded, so it is expected 0, but:%d", n)
	}

	if err := fc.Upload(ctx, "/usr/local/go/bin", filepath.Join("go", sid)); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("./tmp")
	if err := fc.Download(ctx, "./tmp/go", filepath.Join("go", sid)); err != nil {
		t.Fatal(err)
	}
	goStat, err := os.Stat("./tmp/go/go")
	if err != nil {
		t.Fatal(err)
	}
	if goStat.Size() != localStat.Size() {
		t.Fatal("files not match")
	}
}

func TestHttpFileRW(t *testing.T) {
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fc := NewHttpClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}

	f := OpenHttpFile(ctx, "127.0.0.1:1331", filepath.Join("sealed", sid), sid, string(newToken))
	n, err := f.Write([]byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal("expect n == 2")
	}
	f.Close()

	f = OpenHttpFile(ctx, "127.0.0.1:1331", filepath.Join("sealed", sid), sid, string(newToken))
	output, err := ioutil.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "ok" {
		t.Fatalf("read and write don't match:%s", string(output))
	}
}

func TestHttpFileStat(t *testing.T) {
	ctx := context.TODO()
	fc := NewHttpClient("127.0.0.1:1331", "sys", "fdd832c558cab235daaf39b8e59ce41b")
	stat, err := fc.FileStat(ctx, "miner-check.dat")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(stat.Size())
}
