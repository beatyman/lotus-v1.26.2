package model

import "time"

type NodeStatus int32

const (
	NodeStatus_Online NodeStatus = iota
	NodeStatus_Offline
)

// MinerInfo :
type MinerInfo struct {
	ID                      int64  `json:"id"`
	MinerID                 string `json:"miner_id"`
	MinerSize               int64  `json:"miner_size"`
	SectorSize              uint64 `json:"sector_size"`
	RawBytePower            int64  `json:"raw_byte_power"`
	RawByteTotalPower       int64  `json:"raw_byte_total_power"`
	ActualBytePower         int64  `json:"actual_byte_power"`
	ActualByteTotalPower    int64  `json:"actual_byte_toal_power"`
	ActualBytePowerCommited int64  `json:"actual_byte_power_committed"`
	ActualBytePowerProving  int64  `json:"actual_byte_power_proving"`
	MinerBalance            string `json:"miner_balance"`
	MinerBalancePrecommit   string `json:"miner_balance_precommit"`
	MinerBalancePledge      string `json:"miner_balance_pledge"`
	MinerBalanceVesting     string `json:"miner_balance_vesting"`
	MinerBalanceLocked      string `json:"miner_balance_locked"`
	MinerBalanceAvailable   string `json:"miner_balance_available"`
	WorkerBalance           string `json:"worker_balance"`
	MarketBalanceEscrow     string `json:"market_balance_escrow"`
	MarketBalanceLocked     string `json:"market_balance_locked"`
	ExpectedSealDuration    int    `json:"expected_seal_duration"`
	CreateTime              int64  `json:"create_time"`
	PeerId                  string `json:"peer_id"`
}

type WorkerConf struct {
	FixedEnv            string `yaml:"FixedEnv,flow"`
	ID                  string `yaml:"ID,flow"`
	IP                  string `yaml:"IP,flow"`
	SvcUri              string `yaml:"SvcUri,flow"`
	MaxTaskNum          int    `yaml:"MaxTaskNum,flow"`
	ParallelPledge      int    `yaml:"ParallelPledge,flow"`
	ParallelPrecommit1  int    `yaml:"ParallelPrecommit1,flow"`
	ParallelPrecommit2  int    `yaml:"ParallelPrecommit2,flow"`
	ParallelCommit      int    `yaml:"ParallelCommit,flow"`
	Commit2Srv          bool   `yaml:"Commit2Srv,flow"`
	WdPoStSrv           bool   `yaml:"WdPoStSrv,flow"`
	WnPoStSrv           bool   `yaml:"WnPoStSrv,flow"`
	EnvironmentVariable string `yaml:"EnvironmentVariable,flow"`
	AutoInstall         bool   `yaml:"AutoInstall,flow"`
	ParallelFinalize    int    `yaml:"ParallelFinalize,flow"`
	AlwaysDeleteCar     bool   `yaml:"AlwaysDeleteCar,flow"`
}

type WorkerInfo struct {
	*NodeInfo
	WorkerNo           string `json:"worker_no"`           //worker编号
	MinerId            string `json:"miner_id"`            //所属的矿工节点ID 如t01000
	SvcUri             string `json:"svc_uri"`             //worker api地址
	MaxTaskNum         int    `json:"max_task_num"`        //最大任务数
	ParallelPledge     int    `json:"parallel_pledge"`     //addPiece 任务数
	ParallelPrecommit1 int    `json:"parallel_precommit1"` //P1任务数
	ParallelPrecommit2 int    `json:"parallel_precommit2"` //p2任务数
	ParallelCommit     int    `json:"parallel_commit"`     //并行提交 如果数字是0，将选择提交服务直到成功
	Commit2Srv         bool   `json:"commit2_srv"`         //是否开启C2
	WdPostSrv          bool   `json:"wd_post_srv"`         //是否开启WdPost
	WnPostSrv          bool   `json:"wn_post_srv"`         //WnPost
	Disable            bool   `json:"disable"`
}

type WorkerInfoCfg struct {
	ID string // worker id, default is empty for same worker.
	IP string // worker current ip

	//  the seal data
	SvcUri string

	// function switch
	MaxTaskNum         int // need more than 0
	ParallelPledge     int
	ParallelPrecommit1 int // unseal is shared with this parallel
	ParallelPrecommit2 int
	ParallelCommit     int

	Commit2Srv bool // need ParallelCommit2 > 0
	WdPoStSrv  bool
	WnPoStSrv  bool
}

type NodeInfo struct {
	HostIP    string     `json:"host_ip"`    //主机IP
	HostNo    string     `json:"host_no"`    //主机编号
	Version   string     `json:"version"`    //版本号
	Status    NodeStatus `json:"status"`     //运行状态
	Desc      string     `json:"desc"`       //描述
	StartTime time.Time  `json:"start_time"` //启动时间
}

// SectorState :
type SectorState struct {
	ID         int64  `json:"id"`
	MinerID    string `json:"miner_id"`
	WorkerID   string `json:"worker_id"`
	StatusType string `json:"status_type"` //状态类型。有状态机的，有调度的,01.状态机,02.调度
	ClientIP   string `json:"client_ip"`
	SectorID   string `json:"sector_id"`
	SectorSize int64  `json:"sector_size"`
	State      string `json:"state"`
	Msg        string `json:"msg"`
	CreateTime int64  `json:"create_time"`
}

// SectorInfo :
type SectorInfo struct {
	ID         int64  `json:"id"`
	MinerID    string `json:"miner_id"`
	WorkerID   string `json:"worker_id"`
	ClientIP   string `json:"client_ip"`
	SectorID   string `json:"sector_id"`
	SectorSize int64  `json:"sector_size"`
	State      string `json:"state"`
	Msg        string `json:"msg"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"update_time"`
}

// BuriedDataCollectParams :
type BuriedDataCollectParams struct {
	DataType string `json:"data_type"`
	Data     []byte `json:"data"`
}

type KafkaRestData struct {
	Records []KafkaRestValue `json:"records"`
}

type KafkaRestValue struct {
	Value interface{} `json:"value"`
}
