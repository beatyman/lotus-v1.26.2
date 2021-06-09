package proxy

import (
	"bytes"
	"context"
	"encoding/csv"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/util/apiaddr"
	"github.com/filecoin-project/lotus/metrics"
	nauth "github.com/filecoin-project/lotus/node/modules/auth"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/gwaylib/errors"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"
)

const (
	_NODE_ALIVE_CONN_KEY = "check"
)

type LotusNodeApiV1 struct {
	NodeApi api.FullNode
	Closer  jsonrpc.ClientCloser
}
type LotusNodeApiV0 struct {
	NodeApi v0api.FullNode
	Closer  jsonrpc.ClientCloser
}

type LotusNode struct {
	ctx     context.Context
	apiInfo apiaddr.APIInfo

	curHeight int64 // the current epoch of the chain
	usedTimes int   // good times

	lock   sync.Mutex
	v0conn map[string]LotusNodeApiV0
	v1conn map[string]LotusNodeApiV1
}

func (l *LotusNode) CloseAll() error {
	l.lock.Lock()
	defer l.lock.Unlock()

	for _, napi := range l.v0conn {
		napi.Closer()
	}
	for _, napi := range l.v1conn {
		napi.Closer()
	}
	l.v0conn = nil
	l.v1conn = nil

	return nil
}

func (l *LotusNode) IsAlive() bool {
	l.lock.Lock()
	defer l.lock.Unlock()

	return len(l.v0conn) > 0 || len(l.v1conn) > 0
}

func (l *LotusNode) GetNodeApiV0(sessionId string) (*LotusNodeApiV0, error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	conn, ok := l.v0conn[sessionId]
	if ok {
		return &conn, nil
	}

	// only support for v0 to check the chain is it alive.
	addr, err := l.apiInfo.DialArgs("v0", repo.FullNode)
	if err != nil {
		return nil, xerrors.Errorf("could not get DialArgs: %w", err)
	}
	headers := l.apiInfo.AuthHeader()

	// see: github.com/filecoin-project/lotus/api/client/client.go#NewFullNodeRPCV0
	var res v0api.FullNodeStruct
	closer, err := jsonrpc.NewMergeClient(l.ctx, addr, "Filecoin",
		[]interface{}{
			&res.CommonStruct.Internal,
			&res.Internal,
		},
		headers,
	)
	if err != nil {
		return nil, errors.As(err, addr)
	}

	conn = LotusNodeApiV0{
		NodeApi: &res,
		Closer:  closer,
	}
	if l.v0conn == nil {
		l.v0conn = map[string]LotusNodeApiV0{}
	}
	l.v0conn[sessionId] = conn
	return &conn, nil
}
func (l *LotusNode) GetNodeApiV1(sessionId string) (*LotusNodeApiV1, error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	conn, ok := l.v1conn[sessionId]
	if ok {
		return &conn, nil
	}

	// only support for v0 to check the chain is it alive.
	addr, err := l.apiInfo.DialArgs("v1", repo.FullNode)
	if err != nil {
		return nil, xerrors.Errorf("could not get DialArgs: %w", err)
	}
	headers := l.apiInfo.AuthHeader()

	// see: github.com/filecoin-project/lotus/api/client/client.go#NewFullNodeRPCV1
	var res api.FullNodeStruct
	closer, err := jsonrpc.NewMergeClient(l.ctx, addr, "Filecoin",
		[]interface{}{
			&res.CommonStruct.Internal,
			&res.Internal,
		},
		headers,
	)
	if err != nil {
		return nil, errors.As(err, addr)
	}

	conn = LotusNodeApiV1{
		NodeApi: &res,
		Closer:  closer,
	}
	if l.v1conn == nil {
		l.v1conn = map[string]LotusNodeApiV1{}
	}
	l.v1conn[sessionId] = conn
	return &conn, nil
}

var (
	lotusNodesLock = sync.Mutex{}

	lotusProxyCfg    string
	lotusProxyOn     bool
	lotusAutoSelect  bool
	lotusProxyAddr   *apiaddr.APIInfo
	lotusProxyCloser func() error
	lotusNodes       = []*LotusNode{}
	defLotusNode     *LotusNode
	bestLotusNode    *LotusNode
	lotusCheckOnce   sync.Once
	minerp2p         NetConnect
)

func bestNodeApi() api.FullNode {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if !lotusProxyOn {
		node, err := defLotusNode.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
		if err != nil {
			panic(err)
		}
		return node.NodeApi
	}
loop:
	if !lotusAutoSelect {
		if bestLotusNode == nil {
			checkLotusEpoch()
		}
		api, err := bestLotusNode.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
		if err == nil {
			return api.NodeApi
		}
		log.Warn(errors.As(err))
		time.Sleep(1e9)
		goto loop
	}

	for _, node := range lotusNodes {
		if !node.IsAlive() {
			continue
		}
		api, err := node.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
		if err != nil {
			log.Warn(err)
			continue
		}
		return api.NodeApi
	}

	// waitting
	time.Sleep(1e9)
	checkLotusEpoch()
	goto loop
}

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
			apiConn, err := c.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
			if err != nil {
				c.CloseAll()
				log.Warnf("lotus node down:%s", errors.As(err).Error())
				return
			}
			nApi := apiConn.NodeApi
			ts, err := nApi.ChainHead(c.ctx)
			if err != nil {
				c.CloseAll()
				log.Warnf("lotus node down:%s", errors.As(err).Error())
				return
			}
			if !alive {
				if minerp2p != nil {
					remoteAddrs, err := nApi.NetAddrsListen(c.ctx)
					if err != nil {
						log.Warn(xerrors.Errorf("getting full node libp2p address: %w", err))
						return
					}
					log.Infof("minerp2p auto connect to : %s", remoteAddrs)
					if err := minerp2p(c.ctx, remoteAddrs); err != nil {
						log.Warn(errors.As(err))
						return
					}
				}
				log.Infof("lotus node up:%s", c.apiInfo.Addr)
			}
			c.lock.Lock()
			c.curHeight = int64(ts.Height())
			c.lock.Unlock()
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
	if bestLotusNode == nil || lotusAutoSelect {
		changeLotusNode(0)
	}
}

func changeLotusNode(idx int) error {
	// no client set
	if idx > len(lotusNodes)-1 || idx < 0 {
		return errors.New("index not found")
	}
	if bestLotusNode != nil && bestLotusNode.apiInfo.Addr == lotusNodes[idx].apiInfo.Addr {
		return errors.New("no change")
	}
	log.Infof("change lotus node: idx:%d, addr:%s", idx, lotusNodes[idx].apiInfo.Addr)
	bestLotusNode = lotusNodes[idx]
	bestLotusNode.usedTimes++
	return nil
}

func startLotusProxy(addr string, a api.FullNode) (string, func() error, error) {
	if len(addr) == 0 {
		return "", nil, errors.New("not found addr")
	}

	// see: lotus/cmd/lotus/rpc.go
	serverOptions := make([]jsonrpc.ServerOption, 0)
	//if maxRequestSize != 0 { // config set
	//	serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(maxRequestSize))
	//}
	serveRpc := func(path string, hnd interface{}) {
		rpcServer := jsonrpc.NewServer(serverOptions...)
		rpcServer.Register("Filecoin", hnd)

		ah := &auth.Handler{
			Verify: a.AuthVerify,
			Next:   rpcServer.ServeHTTP,
		}

		http.Handle(path, ah)
	}

	pma := api.PermissionedFullAPI(metrics.MetricedFullAPI(a))

	serveRpc("/rpc/v1", pma)
	serveRpc("/rpc/v0", &v0api.WrapperV1Full{FullNode: pma})
	srv := &http.Server{
		Handler: http.DefaultServeMux,
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "lotus-proxy"))
			return ctx
		},
	}
	repo := filepath.Dir(lotusProxyCfg)
	certPath := filepath.Join(repo, "lotus_proxy_crt.pem")
	keyPath := filepath.Join(repo, "lotus_proxy_key.pem")
	if err := nauth.CreateTLSCert(certPath, keyPath); err != nil {
		return "", nil, errors.As(err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, errors.As(err, addr)
	}
	log.Infof("start lotus proxy : %s", ln.Addr())
	arr := strings.Split(ln.Addr().String(), ":")
	port := arr[1]
	go func() {
		defer ln.Close()
		if err := srv.ServeTLS(ln, certPath, keyPath); err != nil {
			log.Warn(errors.As(err))
		}
	}()
	return port, ln.Close, nil
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
	if len(nodes) == 0 {
		return errors.New("client not found")
	}

	proxyAddr := apiaddr.ParseApiInfo(strings.TrimSpace(records[0]))
	return reloadNodes(&proxyAddr, nodes)
}

func reloadNodes(proxyAddr *apiaddr.APIInfo, nodes []*LotusNode) error {
	// no proxy
	if proxyAddr == nil {
		return errors.New("no proxy address to listen")
	}

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
		node.CloseAll()
		log.Infof("remove lotus node:%s", node.apiInfo.String())
	}

	// only support restart the miner to upgrade a new listen
	if lotusProxyAddr != nil {
		return nil
	}
	if !lotusProxyOn {
		lotusProxyAddr = proxyAddr
		return nil
	}
	// start a new proxy
	host, err := proxyAddr.Host()
	if err != nil {
		return errors.As(err, proxyAddr.Addr)
	}
	port, closer, err := startLotusProxy(host, NewLotusProxy(string(proxyAddr.Token)))
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
		decoding := "unknow"
		var syncStat *api.SyncState
		var mpStat []api.ProxyMpStat
		if isAlive {
			apiConn, err := c.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
			if err != nil {
				log.Warn(errors.As(err))
				continue
			}
			nApi := apiConn.NodeApi
			inputName, err := nApi.InputWalletStatus(ctx)
			if err != nil {
				decoding = errors.As(err).Code()
			} else if len(inputName) == 0 {
				decoding = "none"
			} else {
				decoding = inputName
			}
			if cond.ChainSync {
				st, err := nApi.SyncState(ctx)
				if err != nil {
					log.Warn(errors.As(err))
					continue
				}
				syncStat = st
			}
			if cond.ChainMpool {
				stats, err := lotusMpoolStat(ctx, nApi)
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
			Decoding:  decoding,
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

func broadcastMessage(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec) (api.FullNode, *types.SignedMessage, error) {
	sMsg, err := bestNodeApi().MpoolSignMessage(ctx, msg, spec)
	if err != nil {
		return nil, nil, err
	}
	nApi, err := broadcastSignedMessage(ctx, sMsg)
	return nApi, sMsg, err
}

// return the api for wait
func broadcastSignedMessage(ctx context.Context, sm *types.SignedMessage) (api.FullNode, error) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if !lotusProxyOn {
		panic("lotus proxy not on")
	}

	// sync all the data to all node
	var result api.FullNode
	for _, node := range lotusNodes {
		if !node.IsAlive() {
			continue
		}
		apiConn, err := node.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
		if err != nil {
			log.Warn(errors.As(err))
			continue
		}
		nApi := apiConn.NodeApi
		if _, err := nApi.MpoolPush(ctx, sm); err != nil {
			log.Warn(errors.As(err))
			continue
		}
		if result == nil {
			result = nApi
		}
	}
	if result == nil {
		return nil, errors.ErrNoData.As("all nodes down")
	}
	return result, nil
}
