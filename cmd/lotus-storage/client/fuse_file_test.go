package client

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"testing"
)

func TestFileTrailer(t *testing.T) {
	ctx := context.TODO()
	//auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	auth := NewAuthClient("127.0.0.1:1330", "9070f45cc18a162e00719b4dacd76ca1")
	sid := "s-t01003-10000000000"
	newToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fc := NewHttpClient("127.0.0.1:1331", sid, string(newToken))
	if err := fc.DeleteSector(ctx, sid, "all"); err != nil {
		t.Fatal(err)
	}

	f := OpenFUseFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
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

	f = OpenFUseFile("127.0.0.1:1332", filepath.Join("sealed", sid), sid, string(newToken))
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
