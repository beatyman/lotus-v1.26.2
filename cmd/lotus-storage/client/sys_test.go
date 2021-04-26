package client

import (
	"context"
	"testing"
)

func TestAuthClient(t *testing.T) {
	// depned on started the lotus-storage
	ctx := context.TODO()
	auth := NewAuthClient("127.0.0.1:1330", "d41d8cd98f00b204e9800998ecf8427e")
	output, err := auth.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if "all pools are healthy\n" != string(output) {
		t.Fatal(string(output))
	}

	sid := "s-t01003-0"
	newFileToken, err := auth.NewFileToken(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(newFileToken) == 0 {
		t.Fatal("token not found")
	}
	if _, err := auth.DeleteFileToken(ctx, sid); err != nil {
		t.Fatal(err)
	}
}
