package config

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	uuid "github.com/satori/go.uuid"
)

func GetConfigFile() string {
	return GetRootDir() + "/api/config/config.json"
}

var devID = ""
var homeDir = "/etc/helmsman"

func GetHelmsmanEtc() string {
	dir := os.Getenv("HELMSMAN_ETC")
	if len(dir) > 0 {
		return dir
	}

	// update build for windows
	if runtime.GOOS == "windows" {
		homeDir = filepath.Join("c:\\", "helmsman")
	}
	return homeDir
}

func GetDevID() string {
	if len(devID) > 0 {
		return devID
	}

	id, err := ioutil.ReadFile(GetHelmsmanEtc() + "/uuid")
	if err != nil {
		// log.Warn(errors.As(err))
		return CreateDevID()
	}
	devID = string(id)
	return devID
}
func CreateDevID() string {
	devID = uuid.NewV4().String()
	if err := os.Mkdir(homeDir, 0755); err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(GetHelmsmanEtc()+"/uuid", []byte(devID), 0666); err != nil {
		panic(err)
	}
	return devID
}

//配置文件结构体
type Config struct {
	ApiRoot       string   `json:"apiRoot"`       // 登录入口API请求, 用于获取服务器资源
	ApiMain       string   `json:"apiMain"`       // 业务API请求地址，这个是动态返回，可用于负载接入
	HeartBeatTime int      `json:"heartbeatTime"` //心跳
	UpgradeTime   int      `json:"upgradeTime"`   //心跳
	BindStatus    int      `json:"bindStatus"`    //矿机绑定状态
	ApiReport     []string `json:"apiReport"`     //KAFKA接口
	LotusApiUrl string  `json:"lotusApiUrl"`      //lotus节点url
	LotusApiToken string  `json:"lotusApiToken"` // lotus 节点token
	MinerApiUrl string  `json:"minerApiUrl"`  // miner 节点url
	MinerApiToken string  `json:"minerApiToken"` //miner 节点token
	MinerAddress string  `json:"minerAddress"` //旷工钱包地址
	CaptureDurartinSecond int `json:"captureDurartinSecond"` //采集间隔
	IsCaptureLotus int `json:"isCaptureLotus"` // 是否要采集

}

var cfg *Config

func GetConf() *Config {
	if cfg != nil {
		return cfg
	}

	c := &Config{}
	//ReadFile函数会读取文件的全部内容，并将结果以[]byte类型返回
	data, err := ioutil.ReadFile(GetConfigFile())
	if err != nil {
		panic(err)
	}
	//读取的数据为json格式，需要进行解码
	if err := json.Unmarshal(data, c); err != nil {
		panic(err)
	}
	cfg = c
	return cfg
}
func GetConfFile() *Config {
	c := &Config{}
	//ReadFile函数会读取文件的全部内容，并将结果以[]byte类型返回
	data, err := ioutil.ReadFile(GetConfigFile())
	if err != nil {
		panic(err)
	}
	//读取的数据为json格式，需要进行解码
	if err := json.Unmarshal(data, c); err != nil {
		panic(err)
	}
	cfg = c
	return cfg
}

func WriteConf(wConf *Config) {
	cfg = wConf
	cfgData, err := json.MarshalIndent(cfg, "", "	")
	if err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(GetConfigFile(), cfgData, 0666); err != nil {
		panic(err)
	}
}
