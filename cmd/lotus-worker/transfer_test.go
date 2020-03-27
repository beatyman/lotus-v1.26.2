package main

import (
	"context"
	"testing"
)

func TestRsyncLocal(t *testing.T) {
	// return // only test by manu
	ctx := context.Background()
	if err := rsyncLocal(ctx, "/data/nfs/1/cache/s-t0101-101/", "/data/cache/cache/s-t0101-101/"); err != nil {
		t.Fatal(err)
	}
	if err := rsyncLocal(ctx, "/data/nfs/1/sealed/s-t0101-101", "/data/cache/sealed/s-t0101-101"); err != nil {
		t.Fatal(err)
	}
}
