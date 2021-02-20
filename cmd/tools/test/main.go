package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

var _i = 0

func main() {
	fmt.Println("[ctrl+c to exit]")
	end := make(chan os.Signal, 2)
	signal.Notify(end, os.Interrupt, os.Kill)

	start := time.Now()
	fmt.Println(start.Format(time.RFC3339Nano))
	// f3sgcl2v6duorsedqqludj5pstfb3ukoajmefnoddqa3q77fgauc6apcardhhw7jhjz45iae467cdb4zy3tbmq
	// 7b2254797065223a22626c73222c22507269766174654b6579223a22715463346d3353452f44446e685958435979575069515a2f796247616436526a47506434596934436a79513d227d
	src := []byte("7b2254797065223a22626c73222c22507269766174654b6579223a22715463346d3353452f44446e685958435979575069515a2f796247616436526a47506434596934436a79513d227d")

	head := 56
	//clean := len(src) - head // head 56 are same
	for i := len(src) - 1; i > head; i-- {
		src[i] = []byte("0")[0]
	}

	// len = 148
	go func() {
		for {
			select {
			case <-end:
				break
			default:
				to := make([]byte, len(src))
				copy(to, src)
				go find(to)

				increase(src)
				if _i == 1000000000 {
					_i = 0
					fmt.Println(time.Now().Format(time.RFC3339Nano))
					fmt.Println(string(src))
				}
				_i++
			}
		}
		end <- os.Interrupt
	}()
	<-end
	fmt.Println(time.Now().Format(time.RFC3339Nano))
	fmt.Println(string(src))
	fmt.Println(time.Now().Sub(start).String())
}

var limit = make(chan int, runtime.NumCPU())

func find(src []byte) {
	limit <- 1
	defer func() {
		<-limit
	}()
	addr, err := decode(src)
	if err == nil {
		match(addr, src)
	}
}

var (
	num   = []byte("0123456789abcdef")
	index = map[byte]int{
		num[0]:  0,
		num[1]:  1,
		num[2]:  2,
		num[3]:  3,
		num[4]:  4,
		num[5]:  5,
		num[6]:  6,
		num[7]:  7,
		num[8]:  8,
		num[9]:  9,
		num[10]: 10,
		num[11]: 11,
		num[12]: 12,
		num[13]: 13,
		num[14]: 14,
		num[15]: 15,
	}
)

func increase(src []byte) {
	// add
	for j := len(src) - 1; j > -1; j-- {
		idx, _ := index[src[j]]
		if idx == 15 {
			// next
			src[j] = num[0]
			continue
		}
		src[j] = num[idx+1]
		break
	}
}

func match(addr string, src []byte) {
	switch addr {
	case
		"f3sgcl2v6duorsedqqludj5pstfb3ukoajmefnoddqa3q77fgauc6apcardhhw7jhjz45iae467cdb4zy3tbmq",
		"f3tfyy6sph3hiiudxgpmt2kmg6yq2lh52jnulpkysjjgw6anobr25nh4sgphvegne6gxsypy6cxdp732h34era",
		"f3s5i5cjaqqlhblmwohkfwbvcotzriohfcqetnusu7hmiqoliabzsd7626lz3gruywdapteroerg3lleim27wa":
		fmt.Println(string(src))
	}
}

func decode(inpdata []byte) (string, error) {
	var ki types.KeyInfo
	data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(data, &ki); err != nil {
		return "", err
	}

	k, err := wallet.NewKey(ki)
	if err != nil {
		return "", err
	}
	return k.Address.String(), nil
}
