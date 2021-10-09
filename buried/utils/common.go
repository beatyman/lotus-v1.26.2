package utils

import (
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/log"
	"net"
	"os"
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
