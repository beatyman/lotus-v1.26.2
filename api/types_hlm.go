package api

import (
	"time"

	"github.com/filecoin-project/go-state-types/big"
)

type ProxyMpStat struct {
	Addr              string
	Past, Cur, Future uint64
	GasLimit          big.Int
}
type ProxyNode struct {
	Addr     string
	Alive    bool
	Using    bool
	Decoding string

	// for lotus
	Height    int64
	UsedTimes int

	// lotus sync stat
	SyncStat *SyncState

	// local mpool stat
	MpoolStat []ProxyMpStat

	// miner info
}

type ProxyStatCondition struct {
	ChainSync  bool
	ChainMpool bool
}

type ProxyStatus struct {
	ProxyOn    bool
	AutoSelect bool
	Nodes      []ProxyNode
}

type WdPoStLog struct {
	Time time.Time
	Log  string
}
