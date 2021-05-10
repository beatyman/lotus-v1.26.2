package proxy

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"io/ioutil"
	"net"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/cli/util/apiaddr"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
	"golang.org/x/xerrors"
)

type LotusNode struct {
	ctx     context.Context
	apiInfo apiaddr.APIInfo

	curHeight int64 // the current epoch of the chain
	usedTimes int   // good times

	lock sync.Mutex

	nodeApi    v0api.FullNode
	nodeCloser jsonrpc.ClientCloser

	proxyConn net.Conn
}

func (l *LotusNode) Close() error {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.proxyConn != nil {
		database.Close(l.proxyConn)
	}
	l.proxyConn = nil

	if l.nodeCloser != nil {
		l.nodeCloser()
	}
	l.nodeCloser = nil
	l.nodeApi = nil

	return nil
}

func (l *LotusNode) IsAlive() bool {
	return l.nodeApi != nil && l.proxyConn != nil
}

func (l *LotusNode) getConn() (v0api.FullNode, net.Conn, error) {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.proxyConn == nil {
		host, err := l.apiInfo.Host()
		if err != nil {
			return nil, nil, errors.As(err, host)
		}
		conn, err := net.DialTimeout("tcp", host, 30e9)
		if err != nil {
			return nil, nil, errors.As(err, host)
		}
		l.proxyConn = conn
	}

	if l.nodeApi == nil {
		// only support for v0 to check the chain is it alive.
		addr, err := l.apiInfo.DialArgs("v0", repo.FullNode)
		if err != nil {
			return nil, nil, xerrors.Errorf("could not get DialArgs: %w", err)
		}
		headers := l.apiInfo.AuthHeader()
		nApi, closer, err := client.NewFullNodeRPCV0(l.ctx, addr, headers)
		if err != nil {
			return nil, nil, errors.As(err, addr)
		}
		l.nodeApi = nApi
		l.nodeCloser = closer
	}

	return l.nodeApi, l.proxyConn, nil
}

var (
	lotusProxyCfg    string
	lotusProxyOn     bool
	lotusAutoSelect  bool
	lotusProxyAddr   *apiaddr.APIInfo
	lotusProxyCloser func() error
	lotusNodes       = []*LotusNode{}
	bestLotusNode    *LotusNode
	lotusNodesLock   = sync.Mutex{}
	lotusCheckOnce   sync.Once
)

func checkLotusEpoch() {
	defer func() {
		if err := recover(); err != nil {
			log.Error(err)
		}
	}()
	done := make(chan bool, len(lotusNodes))
	for _, client := range lotusNodes {
		go func(c *LotusNode) {
			defer func() {
				done <- true
			}()
			alive := c.IsAlive()
			nApi, _, err := c.getConn()
			if err != nil {
				c.Close()
				log.Warnf("lotus node down:%s", errors.As(err).Error())
				return
			}
			ts, err := nApi.ChainHead(c.ctx)
			if err != nil {
				c.Close()
				log.Warnf("lotus node down:%s", errors.As(err).Error())
				return
			}
			if !alive {
				log.Infof("lotus node up:%s", c.apiInfo.Addr)
			}
			c.curHeight = int64(ts.Height())
		}(client)
	}
	for i := len(lotusNodes); i > 0; i-- {
		<-done
	}
	close(done)

	sort.SliceStable(lotusNodes, func(i, j int) bool {
		if !lotusNodes[i].IsAlive() && lotusNodes[j].IsAlive() {
			return false
		}
		if lotusNodes[i].IsAlive() && !lotusNodes[j].IsAlive() {
			return true
		}

		// inverted order
		return lotusNodes[i].curHeight > lotusNodes[j].curHeight
	})

	// auto select the best one
	if lotusAutoSelect {
		selectBestNode(3)
	}
}

func selectBestNode(diff int64) {
	if bestLotusNode == nil {
		changeLotusNode(0)
		return
	}
	if len(lotusNodes) == 0 {
		// no nodes to compare
		return
	}

	// change the node
	if lotusNodes[0].curHeight-bestLotusNode.curHeight > diff || !bestLotusNode.IsAlive() {
		log.Warnf("the best lotus node %s(alive:%t, height:%d) is unavailable, best lotus node should change to:%s(alive:%t, height:%d)",
			bestLotusNode.apiInfo.Addr, bestLotusNode.IsAlive(), bestLotusNode.curHeight,
			lotusNodes[0].apiInfo.Addr, lotusNodes[0].IsAlive(), lotusNodes[0].curHeight,
		)
		changeLotusNode(0)
	}
	return
}

func changeLotusNode(idx int) {
	// no client set
	if len(lotusNodes)-1 < idx && idx < 0 {
		return
	}
	if bestLotusNode != nil && bestLotusNode.apiInfo.Addr != lotusNodes[idx].apiInfo.Addr {
		// close the connection and let the client do reconnect.
		bestLotusNode.Close()
	}
	log.Infof("change lotus node: idx:%d, addr:%s", idx, lotusNodes[idx].apiInfo.Addr)
	bestLotusNode = lotusNodes[idx]
	bestLotusNode.usedTimes++
	return
}

func startLotusProxy(addr string) (string, func() error, error) {
	if len(addr) == 0 {
		return "", nil, errors.New("not found addr")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, errors.As(err, addr)
	}
	log.Infof("start lotus proxy : %s", ln.Addr())

	exit := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-exit:
				break
			default:
				conn, err := ln.Accept()
				if err != nil {
					// handle error
					log.Warn(errors.As(err))
					continue
				}
				log.Info("DEBUG : accept conn")
				go handleLotus(conn)
			}
		}
	}()
	arr := strings.Split(ln.Addr().String(), ":")
	return arr[1],
		func() error {
			exit <- true
			return ln.Close()
		}, nil
}

func handleLotus(srcConn net.Conn) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if bestLotusNode == nil {
		checkLotusEpoch()
		selectBestNode(3)
	}
	if bestLotusNode == nil {
		srcConn.Close()
		return
	}

	_, targetConn, err := bestLotusNode.getConn()
	if err != nil {
		log.Warn(errors.As(err))
		database.Close(srcConn)
		return
	}

	// copy request.
	// TODO: send the mpool message request to all client, so it will not miss the mpool message.
	go func() {
		if _, err := io.Copy(srcConn, targetConn); err != nil {
			log.Warn(errors.As(err))

			lotusNodesLock.Lock()
			bestLotusNode.Close()
			checkLotusEpoch()
			lotusNodesLock.Unlock()

			srcConn.Close()
		}
	}()

	// copy response.
	go func() {
		if _, err := io.Copy(targetConn, srcConn); err != nil {
			log.Warn(errors.As(err))

			lotusNodesLock.Lock()
			bestLotusNode.Close()
			checkLotusEpoch()
			lotusNodesLock.Unlock()

			srcConn.Close()
		}
	}()
}

func loadLotusProxy(ctx context.Context, cfgFile string) error {
	lotusProxyCfg = cfgFile

	// phare proxy addr
	cfgData, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(cfgFile)
		}
		return errors.As(err, cfgFile)
	}
	r := csv.NewReader(bytes.NewReader(cfgData))
	r.Comment = '#'

	oriRecords, err := r.ReadAll()
	if err != nil {
		return errors.As(err, cfgFile)
	}

	records := []string{}
	for _, line := range oriRecords {
		info := strings.TrimSpace(line[0])
		if len(info) == 0 {
			continue
		}
		records = append(records, info)
	}

	if len(records) < 2 {
		return errors.New("no data or error format").As(len(oriRecords), cfgFile)
	}
	log.Infof("load lotus proxy:%s, len:%d", cfgFile, len(records))

	nodes := []*LotusNode{}
	// the first line is for the proxy addr
	for i := 1; i < len(records); i++ {
		nodes = append(nodes, &LotusNode{
			ctx:     ctx,
			apiInfo: apiaddr.ParseApiInfo(records[i]),
		})
	}

	// checksum the token
	if len(nodes) == 0 {
		return errors.New("client not found")
	}

	// TODO: support different token.
	proxyAddr := apiaddr.ParseApiInfo(strings.TrimSpace(records[0]))
	token := string(proxyAddr.Token)
	for i := len(nodes) - 1; i > 0; i-- {
		if token != string(nodes[i].apiInfo.Token) {
			return errors.New("tokens are not same").As(nodes[i].apiInfo.Addr)
		}
	}

	return reloadNodes(&proxyAddr, nodes)
}

func reloadNodes(proxyAddr *apiaddr.APIInfo, nodes []*LotusNode) error {
	// clean nodes
	removeNodes := []*LotusNode{}
	for _, node := range lotusNodes {
		token := node.apiInfo.String()
		found := -1
		for i, tmpNode := range nodes {
			if tmpNode.apiInfo.String() == token {
				found = i
				break
			}
		}
		if found < 0 {
			removeNodes = append(removeNodes, node)
		} else {
			nodes[found] = node
		}
	}
	lotusNodes = nodes
	for _, node := range removeNodes {
		node.Close()
		log.Infof("remove lotus node:%s", node.apiInfo.String())
	}

	if proxyAddr == nil {
		return nil
	}

	// start the proxy
	if lotusProxyAddr != nil {
		if lotusProxyAddr.String() == proxyAddr.String() {
			// the proxy has not changed
			return nil
		}

		if lotusProxyCloser != nil {
			lotusProxyCloser()
			lotusProxyCloser = nil
			lotusProxyAddr = nil
		}
	}
	// start a new proxy
	host, err := proxyAddr.Host()
	if err != nil {
		return errors.As(err, proxyAddr.Addr)
	}
	port, closer, err := startLotusProxy(host)
	if err != nil {
		return errors.As(err, proxyAddr.Addr)
	}
	proxyAddr.Addr = strings.Replace(proxyAddr.Addr, "/0/", "/"+port+"/", 1)
	lotusProxyCloser = closer
	lotusProxyAddr = proxyAddr
	return nil
}

func lotusProxying() apiaddr.APIInfo {
	if lotusProxyOn {
		if bestLotusNode == nil {
			return apiaddr.APIInfo{}
		}
		return bestLotusNode.apiInfo
	}

	if lotusProxyAddr == nil {
		return apiaddr.APIInfo{}
	}
	return *lotusProxyAddr
}

func lotusProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	// for chain
	proxyingAddr := lotusProxying().Addr
	nodes := []api.ProxyNode{}
	for _, c := range lotusNodes {
		isAlive := c.IsAlive()
		var syncStat *api.SyncState
		var mpStat []api.ProxyMpStat
		if isAlive {
			if cond.ChainSync {
				st, err := c.nodeApi.SyncState(ctx)
				if err != nil {
					log.Warn(errors.As(err))
					continue
				}
				syncStat = st
			}
			if cond.ChainMpool {
				stats, err := lotusMpoolStat(ctx, c.nodeApi)
				if err != nil {
					log.Warn(errors.As(err))
					continue
				}
				mpStat = stats
			}
		}
		nodes = append(nodes, api.ProxyNode{
			Addr:      c.apiInfo.Addr,
			Alive:     c.IsAlive(),
			Using:     c.apiInfo.Addr == proxyingAddr,
			Height:    c.curHeight,
			UsedTimes: c.usedTimes,
			SyncStat:  syncStat,
			MpoolStat: mpStat,
		})
	}

	stat := &api.ProxyStatus{
		ProxyOn:    lotusProxyOn,
		AutoSelect: lotusAutoSelect,
		Nodes:      nodes,
	}
	return stat, nil
}
