package client

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTrailer(t *testing.T) {
	ctx := context.TODO()
	//authData := "d41d8cd98f00b204e9800998ecf8427e"
	authData := "134c58b106f07ed0be86fc5cb860f266"
	auth := NewAuthClient("127.0.0.1:1330", authData)
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fc := NewHttpClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}

	f := OpenFUseFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, fmt.Sprintf("%x", md5.Sum([]byte(authData+"write"))), os.O_RDWR|os.O_CREATE)
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

	if _, err := f.Write([]byte("testing")); err != nil {
		t.Fatal(err)
	}

	if err := f.Truncate(8 + 8 + 4); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	output, err := ioutil.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 8+8+4 {
		t.Fatal(len(output))
	}
	if err := f.Truncate(8 + 8 + 4); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
