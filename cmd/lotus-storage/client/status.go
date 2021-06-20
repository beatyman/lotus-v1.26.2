package client

import (
	"fmt"
	"strconv"
)

func StatisFUse(host string) map[string]string {
	result := map[string]string{}

	fuseSysPools.poolsLk.Lock()
	pool, ok := fuseSysPools.pools[host]
	if ok {
		result[fmt.Sprintf("%s.fuse-sys-pool-size", host)] = strconv.Itoa(pool.Size())
	}
	fuseSysPools.poolsLk.Unlock()

	fuseFilePools.poolsLk.Lock()
	pool, ok = fuseFilePools.pools[host]
	if ok {
		result[fmt.Sprintf("%s.fuse-file-pool-size", host)] = strconv.Itoa(pool.Size())
	}
	fuseFilePools.poolsLk.Unlock()

	return result
}

func StatisFUseAll() map[string]string {
	result := map[string]string{}

	fuseSysPools.poolsLk.Lock()
	for host, pool := range fuseSysPools.pools {
		result[fmt.Sprintf("%s.fuse-sys-pool-size", host)] = strconv.Itoa(pool.Size())
	}
	fuseSysPools.poolsLk.Unlock()

	fuseFilePools.poolsLk.Lock()
	for host, pool := range fuseFilePools.pools {
		result[fmt.Sprintf("%s.fuse-file-pool-size", host)] = strconv.Itoa(pool.Size())
	}
	fuseFilePools.poolsLk.Unlock()

	return result
}
