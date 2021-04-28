package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFileClient(t *testing.T) {
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("./test")

	fc := NewHttpFileClient("127.0.0.1:1331", sid, string(newToken))
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

func TestFileRW(t *testing.T) {
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fc := NewHttpFileClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}

	f := OpenHttpFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
	n, err := f.Write([]byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal("expect n == 2")
	}
	f.Close()

	f = OpenHttpFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
	output, err := ioutil.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "ok" {
		t.Fatalf("read and write don't match:%s", string(output))
	}
}

func TestFileTrailer(t *testing.T) {
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fc := NewFileClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}

	f := OpenFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
	// simulate writeTrailer
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("testing")); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(7)); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(8 + 4); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	f = OpenFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 8+4 {
		t.Fatalf("%d is not in expected", st.Size())
	}
	var tlen [4]byte
	n, err := f.ReadAt(tlen[:], st.Size()-int64(len(tlen)))
	if err != nil {
		t.Fatalf("n:%d, err:%s", n, err)
	}
	f.Close()
}

func TestFileStat(t *testing.T) {
	ctx := context.TODO()
	fc := NewFileClient("127.0.0.1:1331", "sys", "fdd832c558cab235daaf39b8e59ce41b")
	stat, err := fc.FileStat(ctx, "miner-check.dat")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(stat.Size())
}

func TestFileRange(t *testing.T) {
	f := OpenFile("10.41.2.33:1341", "cache/s-t01010-353/sc-02-data-tree-r-last-6.dat", "sys", "caa4ddec7780052bd29001b7119199d6")
	defer f.Close()
	buf := make([]byte, 4096)
	n, err := f.ReadAt(buf, 9586976)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(n)
}
