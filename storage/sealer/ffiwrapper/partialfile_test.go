package ffiwrapper

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/specs-storage/storage"
)

func TestCreateUnsealedFile(t *testing.T) {
	defer os.RemoveAll("./unsealed")
	maxPieceSize := abi.PaddedPieceSize(2048)
	ref := storage.SectorRef{
		SectorFile: storage.SectorFile{
			SectorId:     "s-t01003-0",
			UnsealedRepo: "./",
		},
	}
	f, err := createUnsealedPartialFile(maxPieceSize, ref)
	if err != nil {
		t.Fatal(err)
	}
	// the file size should be 2048+len(uint32) bytes
	stat, err := f.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() != 2048+4 {
		t.Fatalf("expect size:%d, but:%d", 2048+4, stat.Size())
	}

	if err := f.MarkAllocated(0, 512); err != nil {
		t.Fatal(err)
	}
	stat, err = f.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	var tlen [4]byte
	if _, err := f.file.ReadAt(tlen[:], stat.Size()-int64(len(tlen))); err != nil {
		t.Fatal(err)
	}
	trailerLen := binary.LittleEndian.Uint32(tlen[:])
	expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
	if stat.Size() != expectLen {
		t.Fatalf("expect size:%d, but:%d", expectLen, stat.Size())
	}
}

func TestOpenUnsealedFile(t *testing.T) {
	return // need spec key to testing
	maxPieceSize := abi.PaddedPieceSize(2048)

	authUri := "127.0.0.1:1330"
	transfUri := "127.0.0.1:1331"
	key := "d41d8cd98f00b204e9800998ecf8427e"
	//key := "2b4eda3cde069301f88f5384cd55f2f3"
	ctx := context.TODO()
	sid := "s-t01003-0"
	auth := hlmclient.NewAuthClient(authUri, key)
	token, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	f := hlmclient.OpenFile(ctx, transfUri, filepath.Join("unsealed", sid), sid, string(token))
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	var tlen [4]byte
	if _, err := f.ReadAt(tlen[:], stat.Size()-int64(len(tlen))); err != nil {
		t.Fatal(err)
	}
	trailerLen := binary.LittleEndian.Uint32(tlen[:])
	expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
	fmt.Printf("%d,%d,%b\n", stat.Size(), expectLen, tlen)
	if stat.Size() != expectLen {
		t.Fatalf("expect size:%d, but:%d", expectLen, stat.Size())
	}
}
func TestLocalUnsealedFile(t *testing.T) {
	return // need copy the data to ./unsealed/
	maxPieceSize := abi.PaddedPieceSize(2048)
	ref := storage.SectorRef{
		SectorFile: storage.SectorFile{
			SectorId:     "s-t01003-0",
			UnsealedRepo: "./",
		},
	}
	f, err := openUnsealedPartialFile(maxPieceSize, ref)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := f.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	var tlen [4]byte
	if _, err := f.file.ReadAt(tlen[:], stat.Size()-int64(len(tlen))); err != nil {
		t.Fatal(err)
	}
	//if _, err := f.file.Seek(stat.Size()-int64(len(tlen)), 0); err != nil {
	//	t.Fatal(err)
	//}
	//if _, err := f.file.Read(tlen[:]); err != nil {
	//	t.Fatal(err)
	//}
	trailerLen := binary.LittleEndian.Uint32(tlen[:])
	expectLen := int64(trailerLen) + int64(len(tlen)) + int64(maxPieceSize)
	fmt.Printf("%d,%d,%b\n", stat.Size(), expectLen, tlen)
	if stat.Size() != expectLen {
		t.Fatalf("expect size:%d, but:%d", expectLen, stat.Size())
	}
}
