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
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/build"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
	"golang.org/x/xerrors"
)

type LotusNode struct {
	ctx     context.Context
	apiInfo cliutil.APIInfo

	curHeight int64 // the current epoch of the chain
	usedTimes int   // good times

	lock sync.Mutex

	nodeApi    api.FullNode
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

func (l *LotusNode) GetConn() (api.FullNode, net.Conn, error) {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.proxyConn == nil {
		host, err := l.apiInfo.Host()
		if err != nil {
			return nil, nil, errors.As(err)
		}
		conn, err := net.DialTimeout("tcp", host, 30e9)
		if err != nil {
			return nil, nil, errors.As(err)
		}
		l.proxyConn = conn
	}

	if l.nodeApi == nil {
		addr, err := l.apiInfo.DialArgs()
		if err != nil {
			return nil, nil, xerrors.Errorf("could not get DialArgs: %w", err)
		}
		headers := l.apiInfo.AuthHeader()
		nApi, closer, err := client.NewFullNodeRPC(l.ctx, addr, headers)
		if err != nil {
			return nil, nil, errors.As(err)
		}
		l.nodeApi = nApi
		l.nodeCloser = closer
	}

	return l.nodeApi, l.proxyConn, nil
}

var (
	lotusProxyCfg    string
	lotusProxyAddr   *cliutil.APIInfo
	lotusProxyCloser io.Closer
	lotusNodes       = []*LotusNode{}
	bestLotusNode    *LotusNode
	lotusNodesLock   = sync.Mutex{}
)

func checkLotusEpoch() {
	done := make(chan bool, len(lotusNodes))
	for _, client := range lotusNodes {
		go func(c *LotusNode) {
			defer func() {
				done <- true
			}()
			alive := c.IsAlive()
			nApi, _, err := c.GetConn()
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
		return lotusNodes[i].curHeight > lotusNodes[j].curHeight && lotusNodes[i].usedTimes < lotusNodes[j].usedTimes
	})
	// no client set
	if len(lotusNodes) == 0 {
		return
	}

	// change the best client
	if len(lotusNodes) > 1 && bestLotusNode != nil {
		if lotusNodes[0].curHeight-bestLotusNode.curHeight > 3 || !bestLotusNode.IsAlive() {
			log.Warnf("the best lotus node(%s:%t:%d) is unavailable:%d,%t",
				bestLotusNode.apiInfo.Addr, bestLotusNode.IsAlive(), bestLotusNode.curHeight, lotusNodes[0].curHeight,
			)
			bestLotusNode.Close()
			bestLotusNode = nil
		}
	}
	if bestLotusNode == nil {
		log.Infof("change best lotus node to : %s", lotusNodes[0].apiInfo.Addr)
		bestLotusNode = lotusNodes[0]
		bestLotusNode.usedTimes++
	}
	return
}

func startLotusProxy(addr string) (io.Closer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, errors.As(err, addr)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// handle error
				log.Warn(errors.As(err))
				continue
			}
			go handleLotus(conn)
		}
	}()
	go func() {
		tick := time.Tick(time.Duration(build.BlockDelaySecs) * time.Second)
		for {
			lotusNodesLock.Lock()
			checkLotusEpoch()
			lotusNodesLock.Unlock()
			<-tick
		}
	}()
	return ln, nil
}

func handleLotus(srcConn net.Conn) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if bestLotusNode == nil {
		checkLotusEpoch()
	}
	if bestLotusNode == nil {
		srcConn.Close()
		return
	}

	_, targetConn, err := bestLotusNode.GetConn()
	if err != nil {
		log.Warn(errors.As(err))
		baseLotusNode.Close()
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

func LoadLotusProxy(ctx context.Context, cfgFile string) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	return loadLotusProxy(ctx, cfgFile)
}

func RealoadLotusProxy(ctx context.Context) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if len(lotusProxyCfg) == 0 {
		return errors.New("no proxy in running")
	}
	return LoadLotusProxy(ctx, lotusProxyCfg)
}

func loadLotusProxy(ctx context.Context, cfgFile string) error {
	// phare proxy addr
	cfgData, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(cfgFile)
		}
		return errors.As(err)
	}
	r := csv.NewReader(bytes.NewReader(cfgData))
	r.Comment = '#'

	records, err := r.ReadAll()
	if err != nil {
		return errors.As(err)
	}

	if len(records) < 2 {
		return errors.New("no data or error format").As(len(records))
	}

	// the first line is for the proxy addr
	for i := 1; i < len(records); i++ {
		if len(records[i]) != 1 {
			return errors.New("no data or error format").As(records[i])
		}
		lotusNodes = append(lotusNodes, &LotusNode{
			ctx:     ctx,
			apiInfo: cliutil.ParseApiInfo(strings.TrimSpace(records[i][0])),
		})
	}

	// checksum the token
	if len(lotusNodes) == 0 {
		return errors.New("client not found")
	}

	// TODO: support different token.
	proxyAddr := cliutil.ParseApiInfo(strings.TrimSpace(records[0][0]))
	token := string(proxyAddr.Token)
	for i := len(lotusNodes) - 1; i > 0; i-- {
		if token != string(lotusNodes[i].apiInfo.Token) {
			return errors.New("tokens are not same").As(lotusNodes[i].apiInfo.Addr)
		}
	}

	// start the proxy
	if lotusProxyAddr != nil {
		if lotusProxyAddr.String() == proxyAddr.String() {
			// the proxy has not changed
			return nil
		}
		if lotusProxyCloser != nil {
			lotusProxyCloser.Close()
			lotusProxyCloser = nil
			lotusProxyAddr = nil
		}
	}
	// start a new proxy
	host, err := proxyAddr.Host()
	if err != nil {
		return errors.As(err)
	}
	closer, err := startLotusProxy(host)
	if err != nil {
		return errors.As(err)
	}
	log.Infof("using lotus proxy: %s", host)
	lotusProxyCfg = cfgFile
	lotusProxyCloser = closer
	lotusProxyAddr = &proxyAddr
	return nil
}

func LotusProxyStatus(ctx context.Context) []api.ProxyStatus {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	result := []api.ProxyStatus{}

	// best client always at the first position.
	if bestLotusNode != nil {
		result = append(result, api.ProxyStatus{
			Addr:      bestLotusNode.apiInfo.Addr,
			Alive:     bestLotusNode.IsAlive(),
			Height:    bestLotusNode.curHeight,
			UsedTimes: bestLotusNode.usedTimes,
		})
	} else {
		result = append(result, api.ProxyStatus{})
	}

	for _, c := range lotusNodes {
		result = append(result, api.ProxyStatus{
			Addr:      c.apiInfo.Addr,
			Alive:     c.IsAlive(),
			Height:    c.curHeight,
			UsedTimes: c.usedTimes,
		})
	}
	return result
}

func LotusProxyAddr() *cliutil.APIInfo {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	return lotusProxyAddr
}
