package proxy

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/cli/util/apiaddr"
	"github.com/filecoin-project/lotus/node/repo"
)

func TestLotusConnect(t *testing.T) {
	ctx := context.TODO()

	// need build a local lotus, and fix this.
	src := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.kntHArXyTOD8Elak0TcZQfMkiVlmI8a-MmUfDn-jM4k:/ip4/127.0.0.1/tcp/11234/http"
	client := &LotusNode{ctx: ctx, apiInfo: apiaddr.ParseApiInfo(strings.TrimSpace(src))}
	defer client.Close()

	nApi, err := client.getNodeApi()
	if err != nil {
		t.Fatal(err)
	}

	ts, err := nApi.ChainHead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(ts.Height())
}

func TestLotusCfg(t *testing.T) {
	src := `
# the first line is for proxy addr
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.kntHArXyTOD8Elak0TcZQfMkiVlmI8a-MmUfDn-jM4k:/ip4/127.0.0.1/tcp/0/http
# bellow is the cluster node.
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.kntHArXyTOD8Elak0TcZQfMkiVlmI8a-MmUfDn-jM4k:/ip4/127.0.0.1/tcp/11234/http
`
	r := csv.NewReader(strings.NewReader(src))
	//	r.Comma = ''
	r.Comment = '#'

	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	fmt.Print(records)
}

func TestLotusProxy(t *testing.T) {
	ctx := context.TODO()
	if err := UseLotusProxy(ctx, "./lotus.proxy"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1e9)

	pAddr := LotusProxyAddr()
	if pAddr == nil {
		t.Fatal("expect the proxy exist")
	}
	addr, err := pAddr.DialArgs("v0", repo.FullNode)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(addr)
	headers := pAddr.AuthHeader()
	nApi, closer, err := client.NewFullNodeRPCV0(ctx, addr, headers)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := nApi.ChainHead(ctx)
	if err != nil {
		closer()
		t.Fatal(err)
		return
	}
	fmt.Println(ts.Height())
	closer()

	v1Addr, err := pAddr.DialArgs("v1", repo.FullNode)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(v1Addr)
	v1Api, closer, err := client.NewFullNodeRPCV1(ctx, v1Addr, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	ts, err = v1Api.ChainHead(ctx)
	if err != nil {
		t.Fatal(err)
		return
	}
	fmt.Println(ts.Height())
}
