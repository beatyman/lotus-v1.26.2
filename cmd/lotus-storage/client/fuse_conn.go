package client

import (
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/gwaylib/errors"
)

var (
	FUSE_CONN_POOL_SIZE_MAX = 15
)

func init() {
	size, _ := strconv.Atoi(os.Getenv("LOTUS_FUSE_POOL_SIZE"))
	if size > 0 {
		FUSE_CONN_POOL_SIZE_MAX = size
	}
}

type FUseConn struct {
	net.Conn

	forceAllocate bool
}

type FUsePool struct {
	host  string
	queue chan *FUseConn

	owner map[*FUseConn]bool

	size int
	lk   sync.Mutex
}

func (p *FUsePool) new(force bool) (*FUseConn, error) {
	conn, err := net.Dial("tcp", p.host)
	if err != nil {
		return nil, errors.As(err)
	}
	fconn := &FUseConn{
		Conn:          conn,
		forceAllocate: force,
	}
	p.owner[fconn] = true
	p.size += 1

	log.Debugf("create new fuse connection:%s, force:%t, pool size:%d", p.host, force, p.size)
	return fconn, nil
}

func (p *FUsePool) close(conn *FUseConn) error {
	p.size -= 1
	delete(p.owner, conn)
	log.Debugf("close fuse connection:%s, force:%t, pool size:%d", p.host, conn.forceAllocate, p.size)
	return conn.Close()
}
func (p *FUsePool) Close(conn *FUseConn) error {
	p.lk.Lock()
	defer p.lk.Unlock()
	return p.close(conn)
}

func (p *FUsePool) CloseAll() {
	p.lk.Lock()
	defer p.lk.Unlock()
	for c, _ := range p.owner {
		c.Close()
	}
	p.size = 0
	close(p.queue)

	log.Infof("close all fuse connection:%s", p.host)
}

func (p *FUsePool) Borrow(force bool) (*FUseConn, error) {
	p.lk.Lock()
	if len(p.queue) == 0 && (force || p.size < FUSE_CONN_POOL_SIZE_MAX) {
		conn, err := p.new(force)
		if err != nil {
			p.lk.Unlock()
			return nil, errors.As(err)
		}
		p.lk.Unlock()

		if force {
			return conn, nil
		}

		p.queue <- conn
	} else {
		// waiting others return to the pool.
		p.lk.Unlock()
	}

	conn := <-p.queue
	if conn == nil {
		return nil, errors.New("the connection pool has closed")
	}
	return conn, nil
}
func (p *FUsePool) Return(conn *FUseConn) {
	p.lk.Lock()
	if _, ok := p.owner[conn]; !ok {
		p.lk.Unlock()
		return
	}
	// keep the pool size
	if len(p.queue) == FUSE_CONN_POOL_SIZE_MAX {
		p.close(conn)
		p.lk.Unlock()
		return
	}
	p.lk.Unlock()

	p.queue <- conn
}

var (
	FUseConnPoolLk = sync.Mutex{}
	FUseConnPools  = map[string]*FUsePool{}
)

func GetFUseConn(host string, forceAllocate bool) (*FUseConn, error) {
	var pool *FUsePool

	FUseConnPoolLk.Lock()
	pool, _ = FUseConnPools[host]
	if pool == nil {
		pool = &FUsePool{
			host:  host,
			queue: make(chan *FUseConn, FUSE_CONN_POOL_SIZE_MAX),
			owner: map[*FUseConn]bool{},
		}
		FUseConnPools[host] = pool
	}
	FUseConnPoolLk.Unlock()

	return pool.Borrow(forceAllocate)
}
func ReturnFUseConn(host string, conn *FUseConn) {
	var pool *FUsePool

	FUseConnPoolLk.Lock()
	pool, _ = FUseConnPools[host]
	FUseConnPoolLk.Unlock()

	if pool == nil {
		log.Error(errors.ErrNoData.As(host))
		return
	}

	pool.Return(conn)
	return
}
func CloseFUseConn(host string, conn *FUseConn) error {
	var pool *FUsePool

	FUseConnPoolLk.Lock()
	pool, _ = FUseConnPools[host]
	FUseConnPoolLk.Unlock()

	if pool == nil {
		return errors.ErrNoData.As(host)
	}
	return pool.Close(conn)
}
func CloseAllFUseConn(host string) {
	var pool *FUsePool

	FUseConnPoolLk.Lock()
	pool, _ = FUseConnPools[host]
	delete(FUseConnPools, host)
	FUseConnPoolLk.Unlock()

	if pool != nil {
		pool.CloseAll()
	}
}
