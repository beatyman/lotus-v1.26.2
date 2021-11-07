package utils

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

func DeleteC1Out(sector storage.SectorRef) {
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
