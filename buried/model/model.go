package model

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
