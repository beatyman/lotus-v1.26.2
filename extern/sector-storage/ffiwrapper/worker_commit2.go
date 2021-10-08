package ffiwrapper

import (
	"sync"
	"time"
)

var (
	c2cache = &commit2Cache{
		proofs: sync.Map{},
	}
)

func init() {
	go c2cache.loop()
}

type commit2Cache struct {
	proofs sync.Map
}

func (c *commit2Cache) loop() {
	var (
		interval = time.Minute * 10
		expire   = time.Minute * 20
	)

	for {
		<-time.After(interval)

		c.proofs.Range(func(k, v interface{}) bool {
			if out, ok := v.(*Commit2Result); ok {
				if time.Since(out.FinishTime) > expire {
					c.proofs.Delete(k)
				}
			} else {
				c.proofs.Delete(k)
			}
			return true
		})
	}
}

func (c *commit2Cache) set(rst *Commit2Result) {
	if rst == nil || len(rst.Sid) == 0 || len(rst.Proof) == 0 || len(rst.Err) > 0 {
		return
	}
	rst.FinishTime = time.Now()
	c.proofs.Store(rst.Sid, rst)
}

func (c *commit2Cache) get(sid string) (string, string, bool) {
	obj, ok := c.proofs.Load(sid)
	if !ok {
		return "", "", false
	}
	rst, ok := obj.(*Commit2Result)
	if !ok {
		return "", "", false
	}
	return rst.WorkerId, rst.Proof, ok
}
