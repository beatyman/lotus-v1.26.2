package client

import (
	"net"
	"sync"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

const (
	FUSE_CONN_POOL_SIZE_MAX = 12
)

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

func (p *FUsePool) new(force bool) error {
	conn, err := net.Dial("tcp", p.host)
	if err != nil {
		return errors.As(err)
	}
	fconn := &FUseConn{
		Conn:          conn,
		forceAllocate: force,
	}
	p.owner[fconn] = true
	p.size += 1

	log.Infof("create new fuse connection:%s,connections:%d", p.host, p.size)
	p.queue <- fconn
	return nil
}

func (p *FUsePool) close(conn *FUseConn) error {
	p.size -= 1
	delete(p.owner, conn)
	log.Infof("close fuse connection:%s, connections:%d", p.host, p.size)
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
	if len(p.queue) == 0 && (p.size < FUSE_CONN_POOL_SIZE_MAX || force) {
		if err := p.new(force); err != nil {
			p.lk.Unlock()
			return nil, errors.As(err)
		}
	}
	p.lk.Unlock()

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
	if conn.forceAllocate {
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
