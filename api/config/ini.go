package config

import "github.com/gwaylib/conf"
import "github.com/gwaylib/conf/ini"

var (
	Ini = ini.NewIni(conf.RootDir() + "/etc/")
)
