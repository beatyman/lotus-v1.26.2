package client

import (
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/gwaylib/errors"
)

var (
	FUSE_CONN_POOL_SIZE_MAX = 32
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

func (p *FUsePool) Size() int {
	p.lk.Lock()
	defer p.lk.Unlock()
	return p.size
}

func (p *FUsePool) Borrow(force bool) (*FUseConn, error) {
	select {
	case conn := <-p.queue:
		if conn == nil {
			return nil, errors.New("the connection pool has closed")
		}
		return conn, nil
	default:
		p.lk.Lock()

		if !force && p.size > FUSE_CONN_POOL_SIZE_MAX {
			p.lk.Unlock()

			// waiting return the connection.
			conn := <-p.queue
			if conn == nil {
				return nil, errors.New("the connection pool has closed")
			}
			return conn, nil
		}

		// make a new connection
		defer p.lk.Unlock()
		return p.new(force)
	}
}
func (p *FUsePool) Return(conn *FUseConn) {
	p.lk.Lock()
	if _, ok := p.owner[conn]; !ok {
		conn.Close()
		p.lk.Unlock()
		return
	}
	// keep the pool size
	if p.size > FUSE_CONN_POOL_SIZE_MAX && conn.forceAllocate {
		p.close(conn)
		p.lk.Unlock()
		return
	}
	p.lk.Unlock()

	p.queue <- conn
}

type FUsePools struct {
	poolsLk sync.Mutex
	pools   map[string]*FUsePool
}

func NewFUsePools() *FUsePools {
	return &FUsePools{
		pools: map[string]*FUsePool{},
	}
}

func (ps *FUsePools) GetFUseConn(host string, isForce bool) (*FUseConn, error) {
	var pool *FUsePool

	ps.poolsLk.Lock()
	pool, _ = ps.pools[host]
	if pool == nil {
		pool = &FUsePool{
			host:  host,
			queue: make(chan *FUseConn, FUSE_CONN_POOL_SIZE_MAX*2),
			owner: map[*FUseConn]bool{},
		}
		ps.pools[host] = pool
	}
	ps.poolsLk.Unlock()

	return pool.Borrow(isForce)
}
func (ps *FUsePools) ReturnFUseConn(host string, conn *FUseConn) {
	var pool *FUsePool

	ps.poolsLk.Lock()
	pool, _ = ps.pools[host]
	ps.poolsLk.Unlock()

	if pool == nil {
		log.Error(errors.ErrNoData.As(host))
		return
	}

	pool.Return(conn)
	return
}
func (ps *FUsePools) CloseFUseConn(host string, conn *FUseConn) error {
	var pool *FUsePool

	ps.poolsLk.Lock()
	pool, _ = ps.pools[host]
	ps.poolsLk.Unlock()

	if pool == nil {
		return errors.ErrNoData.As(host)
	}
	return pool.Close(conn)
}
func (ps *FUsePools) CloseAllFUseConn(host string) {
	var pool *FUsePool

	ps.poolsLk.Lock()
	pool, _ = ps.pools[host]
	delete(ps.pools, host)
	ps.poolsLk.Unlock()

	if pool != nil {
		pool.CloseAll()
	}
}
