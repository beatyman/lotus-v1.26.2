package utils

import (
	"fmt"
	"github.com/shirou/gopsutil/host"
	"net"
	"os/exec"
	"strings"
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
