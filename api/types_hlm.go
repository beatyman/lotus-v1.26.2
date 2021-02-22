package api

type ProxyStatus struct {
	Addr  string
	Alive bool

	// for lotus
	Height    int64
	UsedTimes int

	// lotus sync status
	SyncStat *SyncState
}
