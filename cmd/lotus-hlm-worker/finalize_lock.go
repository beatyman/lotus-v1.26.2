package main

import (
	"sync/atomic"
)

func (w *worker) TryLockFinalize() {
	w.finalizeCond.L.Lock()
	defer w.finalizeCond.L.Unlock()
	for  {
		currentCount := atomic.LoadInt64(&w.finalizeCount)
		if currentCount < int64(w.workerCfg.ParallelFinalize) {
			atomic.AddInt64(&w.finalizeCount, 1)
			break
		}
		w.finalizeCond.Wait()
	}
}

func (w *worker) TryReleaseFinalize() {
	w.finalizeCond.L.Lock()
	defer w.finalizeCond.L.Unlock()
	atomic.AddInt64(&w.finalizeCount, -1)
	w.finalizeCond.Broadcast()
}