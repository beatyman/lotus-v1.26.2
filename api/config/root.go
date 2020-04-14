package config

import (
	"os"
	"strings"
)

// 获取项目根目录
func GetRootDir() string {
	prjRoot := os.Getenv("PRJ_ROOT")
	if len(prjRoot) == 0 {
		return "."
	}
	if strings.HasSuffix(prjRoot, "/") {
		return string(prjRoot[:len(prjRoot)-1])
	}
	return prjRoot
}
