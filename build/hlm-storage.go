package build

import (
	"sync"
	"time"
)

var (
	timeoutLk           = sync.Mutex{}
	ProvingCheckTimeout = 30 * time.Second
	faultCheckTimeout   = 60 * time.Second
)

func SetProvingCheckTimeout(timeout time.Duration) {
	timeoutLk.Lock()
	defer timeoutLk.Unlock()
	ProvingCheckTimeout = timeout
}
func GetProvingCheckTimeout() time.Duration {
	timeoutLk.Lock()
	defer timeoutLk.Unlock()
	return ProvingCheckTimeout
}
func SetFaultCheckTimeout(timeout time.Duration) {
	timeoutLk.Lock()
	defer timeoutLk.Unlock()
	faultCheckTimeout = timeout
}
func GetFaultCheckTimeout() time.Duration {
	timeoutLk.Lock()
	defer timeoutLk.Unlock()
	return faultCheckTimeout
}
