package proxy

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/lotus/api/client"
)

func TestLotusConnect(t *testing.T) {
	ctx := context.TODO()

	// need build a local lotus, and fix this.
	src := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.YxbCtlsKQI_lbCVh8JajNwJZ_vc38f3viqpa3GYO1S8:/ip4/127.0.0.1/tcp/11234/http"
	RegisterLotus(ctx, src)

	client, err := GetBestLotusProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	nApi, _, err := client.GetConn()
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
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.YxbCtlsKQI_lbCVh8JajNwJZ_vc38f3viqpa3GYO1S8:/ip4/127.0.0.1/tcp/1345/http
# bellow is the cluster node.
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.YxbCtlsKQI_lbCVh8JajNwJZ_vc38f3viqpa3GYO1S8:/ip4/127.0.0.1/tcp/11234/http
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
	if err := LoadLotusProxy(ctx, "./lotus.proxy"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1e9)

	pAddr := GetLotusProxy()
	if pAddr == nil {
		t.Fatal("expect the proxy exist")
	}
	addr, err := pAddr.DialArgs()
	if err != nil {
		t.Fatal(err)
	}
	headers := pAddr.AuthHeader()
	nApi, closer, err := client.NewFullNodeRPC(ctx, addr, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	ts, err := nApi.ChainHead(ctx)
	if err != nil {
		t.Fatal(err)
		return
	}
	fmt.Println(ts.Height())
}
