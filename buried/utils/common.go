package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
	"github.com/shirou/gopsutil/host"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var workerToken = ""
var workerUrl = ""

var longLetters = []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandLow 随机字符串，包含 1~9 和 a~z - [i,l,o]
func RandLow(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	arc := uint8(0)
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	for i, x := range b {
		arc = x & 31
		b[i] = longLetters[arc]
	}
	return string(b)
}

// 获取本机网卡IP
func GetLocalIP() (ipv4 string, err error) {
	var (
		addrs   []net.Addr
		addr    net.Addr
		ipNet   *net.IPNet // IP地址
		isIpNet bool
	)
	// 获取所有网卡
	if addrs, err = net.InterfaceAddrs(); err != nil {
		return
	}
	// 取第一个非lo的网卡IP
	for _, addr = range addrs {
		// 这个网络地址是IP地址: ipv4, ipv6
		if ipNet, isIpNet = addr.(*net.IPNet); isIpNet && !ipNet.IP.IsLoopback() {
			// 跳过IPV6
			if ipNet.IP.To4() != nil {
				ipv4 = ipNet.IP.String() // 192.168.1.1
				return
			}
		}
	}

	//err = common.ERR_NO_LOCAL_IP_FOUND
	return
}

func DeleteC1Out(sector storiface.SectorRef) {
	//判断C1输出文件是否存在，如果存在，则跳过C1
	pathTxt := sector.CachePath() + "/c1.out"
	isExist, err := ffiwrapper.PathExists(pathTxt)
	if err != nil {
		log.Error(pathTxt+" C1 PathExists Err :", err)
	}
	if isExist {
		os.Remove(pathTxt)
	}
}

func GetHostNo() (string, error) {
	host, err := host.Info()
	if err != nil {
		return "", err
	}
	return host.HostID, nil
}

func ExeSysCommand(cmdStr string) string {
	cmd := exec.Command("sh", "-c", cmdStr)
	opBytes, err := cmd.Output()
	if err != nil {
		fmt.Println(err)
		return ""
	}
	smartctlInfo := strings.Trim(string(opBytes), "\n")
	return smartctlInfo
}

func GetMinerAddr() string {
	url, token, err := GetWorkerAddrAndToken()
	if err != nil {
		log.Error("err ===============GetWorkerAddrAndToken()===================", err)
		return ""
	}
	data := new(DataJson)
	data.Method = "Filecoin.ActorAddress"
	dataByte, _ := json.Marshal(data)
	str, err := RequsetUrl("POST", url, token, string(dataByte))
	if err != nil {
		log.Error("err ===============RequsetUrl(POST, url, token, string(dataByte))===================", err)
		return ""
	}

	var resp ActorAddressResp
	if err = json.Unmarshal(str, &resp); err != nil {
		log.Error("err ===============json.Unmarshal(str, &resp)===================", err)
		return ""
	}
	return resp.Result
}

func RequsetUrl(method string, url string, token string, dataJson string) ([]byte, error) {
	buffer := bytes.NewBuffer([]byte(dataJson))
	request, err := http.NewRequest(method, url, buffer)
	if err != nil {
		return nil, errors.As(err, "http.NewRequest failure", method, url, dataJson)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{}
	resp, err := client.Do(request.WithContext(context.TODO()))
	if err != nil {
		return nil, errors.As(err, method, url, dataJson)
	}
	defer resp.Body.Close()
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err, "ioutil.ReadAll failure")
	}
	return respBytes, nil
}

func GetWorkerJwt() (addr string, token string, err error) {
	if len(workerToken) > 0 {
		return workerUrl, workerToken, nil
	}
	url, token, err := GetWorkerAddrAndToken()
	if err != nil {
		return "", "", err
	}
	workerUrl = url
	workerToken = token
	return workerUrl, workerToken, nil
}

func GetWorkerAddrAndToken() (api string, token string, err error) {
	//path := "/data/sdb/lotus-user-1/"
	path := "/data/sdb/lotus-user-1/.lotusstorage"

	tokenPath := path + "/worker_token"
	urlPath := path + "/worker_api"
	tokenBytes, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		log.Error("err ===============ioutil.ReadFile(tokenPath)===================", err)
		return "", "", err
	}
	token = string(tokenBytes)
	token = strings.Trim(token, "")
	token = strings.Trim(token, "\n")
	token = strings.Trim(token, "\\n")
	urlBytes, err := ioutil.ReadFile(urlPath)
	if err != nil {
		log.Error("err ===============ioutil.ReadFile(urlPath)===================", err)
		return "", "", err
	}

	addr := string(urlBytes)
	//str := "/ip4/10.41.1.14/tcp/11234/http"
	split := strings.Split(addr, "/")
	api = "http://" + split[2] + ":" + split[4] + "/rpc/v0"

	return api, token, nil
}

type DataJson struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	Id      uint64        `json:"id"`
}

type ActorAddressResp struct {
	Jsonrpc string `json:"jsonrpc"`
	Result  string `json:"result"`
	Id      uint64 `json:"id"`
}

func Tracefile(str_content string, path string) {
	log.Info("=========================Tracefile===================================", path, "==========", str_content)
	fd, _ := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	fd_time := time.Now().Format("2006-01-02 15:04:05")
	fd_content := strings.Join([]string{"======", fd_time, "=====", str_content, "\n"}, "")
	buf := []byte(fd_content)
	fd.Write(buf)
	fd.Close()
}

func JudgeProofMethod(proof abi.RegisteredSealProof) (bool, string) {
	if proof == abi.RegisteredSealProof_StackedDrg32GiBV1_1 {
		return true, ""
	}
	if proof == abi.RegisteredSealProof_StackedDrg64GiBV1_1 {
		return true, ""
	}
	if proof == abi.RegisteredSealProof_StackedDrg2KiBV1_1 {
		return true, ""
	}

	return false, " sector proof type " + strconv.FormatInt(int64(proof), 10)

}

func substr(s string, pos, length int) string {
	runes := []rune(s)
	l := pos + length
	if l > len(runes) {
		l = len(runes)
	}
	return string(runes[pos:l])
}

func GetParentDirectory(dirctory string) string {
	return substr(dirctory, 0, strings.LastIndex(dirctory, "/"))
}

func GetCurrentDirectory() string {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	return strings.Replace(dir, "\\", "/", -1)
}
