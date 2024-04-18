package ffiwrapper

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
)

const (
	Fail    = "fail"
	Being   = "being"
	Running = "running"
	Success = "success"
)

type c2BodyRequest struct {
	ActorID   uint64 `json:"ActorID"`
	SectorID  uint64 `json:"SectorID"`
	Phase1Out []byte `json:"Phase1Out"`
	Timestamp int64  `json:"Timestamp"`
	Nonce     string `json:"Nonce"`
	Sign      string `json:"Sign"`
}

type c2UpdateBodyRequest struct {
	ActorID         uint64   `json:"ActorID"`
	SectorID        uint64   `json:"SectorID"`
	UpdateProofType int64    `json:"UpdateProofType"`
	SectorKey       []byte   `json:"SectorKey"`
	NewSealed       []byte   `json:"NewSealed"`
	NewUnsealed     []byte   `json:"NewUnsealed"`
	VanillaProofs   [][]byte `json:"VanillaProofs"`
	Timestamp       int64    `json:"Timestamp"`
	Nonce           string   `json:"Nonce"`
	Sign            string   `json:"Sign"`
}

type c2BodyResponse struct {
	Proof []byte `json:"Proof"`
	State string `json:"State"`
}

func requestHttp(method string, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(string(respBody))
	}
	return respBody, nil
}

func requestHttpGzip(method string, url string, body []byte) ([]byte, error) {
	var zBuf bytes.Buffer
	zw := gzip.NewWriter(&zBuf)
	if _, err := zw.Write(body); err != nil {
		return nil, err
	}
	zw.Close()
	req, err := http.NewRequest(method, url, &zBuf)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(string(respBody))
	}
	return respBody, nil
}

type mapEntryHandler func(string, interface{})

func traverseMapInStringOrder(params map[string]interface{}, handler mapEntryHandler) {
	keys := make([]string, 0)
	for k, _ := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		handler(k, params[k])
	}
}

func generateMd5(str string) string {
	s := md5.New()
	s.Write([]byte(str))
	return hex.EncodeToString(s.Sum(nil))
}

func sign(obj interface{}, token string) (string, error) {
	jsonStr, err := json.Marshal(obj)
	if err != nil {
		return "", errors.New("json parse error")
	}
	var data map[string]interface{}
	err = json.Unmarshal(jsonStr, &data)
	if err != nil {
		return "", errors.New("json parse error")
	}
	origin := ""
	traverseMapInStringOrder(data, func(key string, value interface{}) {
		if key != "Sign" {
			origin += fmt.Sprintf("%v", value)
		}
	})
	origin += token
	return generateMd5(origin), nil
}
