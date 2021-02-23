package api

type ProxyNode struct {
	Addr  string
	Alive bool
	Using bool

	// for lotus
	Height    int64
	UsedTimes int

	// lotus sync status
	SyncStat *SyncState
}

type ProxyStatus struct {
	ProxyOn    bool
	AutoSelect bool
	Nodes      []ProxyNode
}
