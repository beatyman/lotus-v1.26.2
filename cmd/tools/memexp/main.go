package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

func trimLeft(src []byte) []byte {
	for idx, b := range src {
		if b != 0x00 {
			return src[idx:]
		}
	}
	return []byte{}
}

func hexDecode(in string) []byte {
	h, err := hex.DecodeString(in)
	if err != nil {
		panic(err)
	}
	return h
}

func getAddr(target []byte) (*wallet.Key, error) {
	defer func() {
		if err := recover(); err != nil {
			//fmt.Println(err)
		}
	}()

	var key *wallet.Key
	var err error
	for i := 0; i < 5; i++ {
		kInfo := types.KeyInfo{Type: "bls", PrivateKey: target}
		key, err = wallet.NewKey(context.TODO(), kInfo)
		if err == nil {
			return key, nil
		}
		target = append(target, 0)
	}
	return key, err
}

func outputKey(key *wallet.Key) {
	kInfoData, err := json.Marshal(key.KeyInfo)
	if err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(fmt.Sprintf("%s.dat", key.Address), []byte(hex.EncodeToString(kInfoData)), 0600); err != nil {
		panic(err)
	}
}

func findKey(hexKey string) {
	target := hexDecode(hexKey)

	dirs, err := ioutil.ReadDir("./")
	if err != nil {
		panic(err)
	}

	for _, f := range dirs {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.Index(name, "dump") < 0 {
			continue
		}
		data, err := ioutil.ReadFile(name)
		if err != nil {
			panic(err)
		}

		foundIdx := 0
	nextPos:
		found := bytes.Index(data[foundIdx:], target)
		if found < 0 {
			continue
		}
		nextIdx := foundIdx + found
		//fmt.Printf("%s,%d,%x\n", name, nextIdx, data[nextIdx+len(target)])
		if err := ioutil.WriteFile(fmt.Sprintf("%s-%d.hex", name, nextIdx), []byte(hex.EncodeToString(data[nextIdx-200:nextIdx+200])), 0666); err != nil {
			panic(err)
		}
		foundIdx = nextIdx + len(target)
		goto nextPos
	}
}

func findData(file string, KeyData []byte) int {
	startTarget, err := hex.DecodeString("000000000000")
	if err != nil {
		panic(err)
	}
	endTarget, err := hex.DecodeString("000000000000")
	if err != nil {
		panic(err)
	}
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	rBufLen := 100 * 1024 * 1024 // 100MB
	bufLen := 2 * rBufLen
	buf := make([]byte, bufLen) // 200MB
	bufFillIdx := 0
	fileIdx := 0
	findIdx := 0
	processPart := 0
loopReadFile:
	for {
		process := fileIdx / (1024 * 1024 * 1024)
		if process != processPart {
			processPart = process
			fmt.Printf("%s [log] read %dGB\n", time.Now().Format(time.RFC3339), process)
		}
		n, err := f.ReadAt(buf[bufFillIdx:], int64(fileIdx))
		fileIdx += n
		if err != nil {
			if io.EOF != err {
				panic(err)
			}
		}
		if n == 0 {
			break
		}
	bufStart:
		tmpIdx := bytes.Index(buf[findIdx:], startTarget)
		if tmpIdx > -1 {
			bakIdx := findIdx + tmpIdx
			findIdx = bakIdx + len(startTarget)
			// search data
			// find the data area
			for {
				if findIdx >= bufLen {
					copy(buf, buf[bakIdx:])
					bufFillIdx = bakIdx
					goto loopReadFile
				}
				if buf[findIdx] > 0x0 {
					// found the data area
					break
				}
				findIdx++
			}
			// search end
			endFound := bytes.Index(buf[findIdx:], endTarget)
			if endFound < 0 {
				if findIdx+len(endTarget) >= bufLen {
					copy(buf, buf[findIdx:])
					bufFillIdx = findIdx
					goto loopReadFile
				}
				goto bufStart
			}
			// found the data
			foundData := buf[findIdx : findIdx+endFound]
			kInfo, _ := getAddr(foundData)
			if kInfo != nil {
				addr := kInfo.Address.String()
				switch addr {
				case "f3qfsp2vklbxwytnnsuwpegta72itfmq5tzdy5yaqv5qaa3lmsa4p7zx5pnbb334ww4vjbju2lbhzk43wxx2jq", // 2k
					"f3tfiwi6r26yp7xu5fgz6dds4yj4e5flibtzkr5tgl2k4psfq256rzl64zbgavuz6tkrlwn3oyih6uslfxio3q", // 2k

					"f3r5rs37pe3226ljqxi3elfsc6c3az265ivpn442doxkwtbspfss3tctcrbzpnwq4spwkkpcv5mzyekcnwk5rq", // 868 owner
					"f3s2irsq2flbc4hjmqydv36vgiacph2tlfbkb4fbyfh2mynuhxrityz3t5yngnmzyzgcuihbdhnrwwk4ibc3fq", // 868 worker
					"f3rwot3jc4kvr6x4dqthphru22l6vqa6l4vpdzhtbakkry7yujeujsyv66f55rmdpzr67mfdmtzqkalyvusn6a", // 868 control-0

					"f3qdj3owwft74uulhjapjcxpnk2to63gpato47sxg4mjcnzjuv7brnita4g6xo6goo2nxqiuecnniyyocffhna", // calibnet
					"f3qhah4yeidcqfm62pk4srtyiiod2fons4gur5q62psd2mocgnk52lq6lced7mmyoa4crriycyl2pumckynpxa", // calibnet
					"f3qkk7b3istrosgaiefsaol4cusio5txqpprgnptijpni43vqfcgfc6tv7iskhmej5xavrwp44pdg6ne4j7cta", // calibnet
					"f3snbmzx53ikzl6tatduimnxi7xz3z7jhpjsbvxishuuhvoeg6wxlflm4appaptm3k46l7hxvusc7oylt2zs2q", // calibnet
					"f3vadzto77leneiblnjzlrd2kaoz6brb324l4j5rl6z7qjtejpjj2kvbcw5wv2ttirtye4kb4bpjc7oedyj6da", // calibnet
					"f3wao6k4b64bcztrn27b3o45zrhrq5w5sh6rbsonanixhku6oomlwohxzx35n7bfwvbw3evaciluhe5vvcaiqq": // calibnet
					fmt.Printf("match:%s\n", addr)
					outputKey(kInfo)
				}
			}
			goto bufStart
		}

		copy(buf, buf[rBufLen-len(startTarget):]) // copy the right half part to the left part for continue read
		bufFillIdx = len(startTarget)             // reset the fill index
		findIdx = 0                               // reset the find index
	}
	return -1
}

var fileMode bool

func init() {
	flag.BoolVar(&fileMode, "file-mode", false, "using file mode")
}

func main() {
	flag.Parse()
	fmt.Printf("%s [log] start\n", time.Now().Format(time.RFC3339))
	//t1,err:=hex.DecodeString("597d77f81f60966202ccc07e13dadd20b4d72fe2dffd74043696128926759c5c")
	//if err != nil{
	//      panic(err)
	//}
	//fmt.Println(string(t1))
	//return

	// t3-1 : fe9a0e2cda5d964e219b67e920f943690ebb900010c715554179bd57770aa04c
	// t3 : 2947fa383b04e77145ac7edcb5f2a4a0ef385d5b6ab7af2545f9a2e5be8cd641
	//findKey("2947fa383b04e77145ac7edcb5f2a4a0ef385d5b6ab7af2545f9a2e5be8cd641")
	//outputKey("2947fa383b04e77145ac7edcb5f2a4a0ef385d5b6ab7af2545f9a2e5be8cd641")
	//findKey("a64db06925388ac771dc4f2fb0528b0b875463de0f06a4b566ccaa4143394267")
	//findKey("662af448f3373f8c9600ac4b27fc6f933f226ac0f67719248116cc2a81e4085b")
	//findKey("7aa8a1e1f42497ddf53614ce981df4ff9035ffa87862935a3d11ae52c0c1b200")
	//return

	keyData, err := ioutil.ReadFile("/tmp/t3.json")
	if err != nil {
		panic(err)
	}
	info := &types.KeyInfo{}
	if err := json.Unmarshal(keyData, info); err != nil {
		panic(err)
	}
	key, err := getAddr(info.PrivateKey)
	if err != nil {
		panic(err)
	}
	fmt.Printf("PrivateKey:%x, addr:%s\n", info.PrivateKey, key.Address)
	//return

	dirs, err := ioutil.ReadDir("./")
	if err != nil {
		panic(err)
	}

	//startTarget, err := hex.DecodeString("003500000000000000")
	//if err != nil {
	//      panic(err)
	//}
	startTarget, err := hex.DecodeString("000000000000")
	if err != nil {
		panic(err)
	}
	endTarget, err := hex.DecodeString("000000000000")
	if err != nil {
		panic(err)
	}
	//startTarget = info.PrivateKey
loopDir:
	for _, f := range dirs {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.Index(name, "dump") < 0 {
			continue
		}
		if fileMode {
			findData(name, info.PrivateKey)
			continue
		}

		data, err := ioutil.ReadFile(name)
		if err != nil {
			panic(err)
		}

		nextIdx := 0
	nextPos:
		found := bytes.Index(data[nextIdx:], startTarget)
		if found < 0 {
			continue
		}
		dataIdx := nextIdx + found + len(startTarget)

		// find the data area
		for {
			if dataIdx >= len(data) {
				continue loopDir
			}
			if data[dataIdx] > 0x0 {
				break
			}
			dataIdx++
		}
		nextIdx = dataIdx

		endFound := bytes.Index(data[dataIdx:], endTarget)
		if endFound < 0 {
			goto nextPos
		}
		nextIdx = dataIdx + endFound
		foundData := data[dataIdx : dataIdx+endFound]
		//if bytes.Index(data[dataIdx:dataIdx+endFound], info.PrivateKey) > -1 {
		//	fmt.Printf("%x,%x\n", data[dataIdx:dataIdx+endFound], info.PrivateKey)
		//}
		kInfo, err := getAddr(foundData)
		if err != nil {
			goto nextPos
		}
		if kInfo != nil {
			addr := kInfo.Address.String()
			switch addr {
			case "f3qfsp2vklbxwytnnsuwpegta72itfmq5tzdy5yaqv5qaa3lmsa4p7zx5pnbb334ww4vjbju2lbhzk43wxx2jq",
				"f3tfiwi6r26yp7xu5fgz6dds4yj4e5flibtzkr5tgl2k4psfq256rzl64zbgavuz6tkrlwn3oyih6uslfxio3q",

				"f3r5rs37pe3226ljqxi3elfsc6c3az265ivpn442doxkwtbspfss3tctcrbzpnwq4spwkkpcv5mzyekcnwk5rq", // 868 owner
				"f3s2irsq2flbc4hjmqydv36vgiacph2tlfbkb4fbyfh2mynuhxrityz3t5yngnmzyzgcuihbdhnrwwk4ibc3fq", // 868 worker
				"f3rwot3jc4kvr6x4dqthphru22l6vqa6l4vpdzhtbakkry7yujeujsyv66f55rmdpzr67mfdmtzqkalyvusn6a", // 868 control-0

				"f3qdj3owwft74uulhjapjcxpnk2to63gpato47sxg4mjcnzjuv7brnita4g6xo6goo2nxqiuecnniyyocffhna",
				"f3qhah4yeidcqfm62pk4srtyiiod2fons4gur5q62psd2mocgnk52lq6lced7mmyoa4crriycyl2pumckynpxa",
				"f3qkk7b3istrosgaiefsaol4cusio5txqpprgnptijpni43vqfcgfc6tv7iskhmej5xavrwp44pdg6ne4j7cta",
				"f3snbmzx53ikzl6tatduimnxi7xz3z7jhpjsbvxishuuhvoeg6wxlflm4appaptm3k46l7hxvusc7oylt2zs2q",
				"f3vadzto77leneiblnjzlrd2kaoz6brb324l4j5rl6z7qjtejpjj2kvbcw5wv2ttirtye4kb4bpjc7oedyj6da",
				"f3wao6k4b64bcztrn27b3o45zrhrq5w5sh6rbsonanixhku6oomlwohxzx35n7bfwvbw3evaciluhe5vvcaiqq":
				fmt.Printf("match:%s\n", addr)
				outputKey(kInfo)
			}
		}
		goto nextPos
	}
	fmt.Printf("%s [log] end\n", time.Now().Format(time.RFC3339))
}
