package cliutil

import "testing"

func TestApiInfo(t *testing.T) {
	src := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.xethCmsSzmRWHw7JANmQxcnyA9uZaEsVzzoI1nR9nPU:/ip4/127.0.0.1/tcp/11234/http"
	info := ParseApiInfo(src)

	if info.String() != src {
		t.Fatal("format not match")
	}

	if info.Addr != "/ip4/127.0.0.1/tcp/11234/http" {
		t.Fatal("addr not match")
	}
}
