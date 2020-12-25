package api

import "time"

type ProxyStatus struct {
	Addr  string
	Alive bool

	// for lotus
	Height int64
	Escape time.Duration
}
