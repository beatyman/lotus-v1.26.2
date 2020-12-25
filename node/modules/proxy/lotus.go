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

type LotusClient struct {
	ctx     context.Context
	apiInfo cliutil.APIInfo

	curHeight int64 // the current epoch of the chain

	lock sync.Mutex

	nodeApi    api.FullNode
	nodeCloser jsonrpc.ClientCloser

	proxyConn net.Conn
}

func (l *LotusClient) Close() error {
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

func (l *LotusClient) IsAlive() bool {
	return l.nodeApi != nil && l.proxyConn != nil
}

func (l *LotusClient) GetConn() (api.FullNode, net.Conn, error) {
	l.lock.Lock()
	defer l.lock.Unlock()

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

	if l.proxyConn == nil {
		host, err := l.apiInfo.Host()
		if err != nil {
			return nil, nil, errors.As(err)
		}
		conn, err := net.Dial("tcp", host)
		if err != nil {
			return nil, nil, errors.As(err)
		}
		l.proxyConn = conn
	}

	return l.nodeApi, l.proxyConn, nil
}

var (
	lotusProxyCfg    string
	lotusProxyAddr   *cliutil.APIInfo
	lotusProxyCloser io.Closer
	lotusClients     = []LotusClient{}
	bestClient       *LotusClient
	lotusClientsLock = sync.Mutex{}
)

func checkLotusEpoch() {
	done := make(chan bool, len(lotusClients))
	for _, client := range lotusClients {
		go func(c *LotusClient) {
			defer func() {
				done <- true
			}()
			nApi, _, err := c.GetConn()
			if err != nil {
				log.Warn(errors.As(err))
				return
			}
			ts, err := nApi.ChainHead(c.ctx)
			if err != nil {
				log.Warn(errors.As(err))
				return
			}
			c.curHeight = int64(ts.Height())
		}(&client)
	}
	for i := len(lotusClients); i > 0; i-- {
		<-done
	}
	close(done)

	sort.SliceStable(lotusClients, func(i, j int) bool {
		// inverted order
		return lotusClients[i].curHeight > lotusClients[j].curHeight
	})
	// no client set
	if len(lotusClients) == 0 {
		return
	}

	// change the best client
	if len(lotusClients) > 1 && bestClient != nil {
		if bestClient.curHeight-lotusClients[0].curHeight > 3 {
			bestClient.Close()
			bestClient = nil
		}
	}
	if bestClient == nil {
		bestClient = &lotusClients[0]
	}
	return
}

// TODO: replace the config
func RegisterLotus(ctx context.Context, apiInfo string) {
	lotusClientsLock.Lock()
	lotusClients = append(lotusClients, LotusClient{
		ctx:     ctx,
		apiInfo: cliutil.ParseApiInfo(apiInfo),
	})
	lotusClientsLock.Unlock()

}

func startLotusProxy(addr string) (io.Closer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, errors.As(err, addr)
	}
	go func() {
		timer := time.NewTimer(time.Duration(build.BlockDelaySecs) * time.Second)
		for {
			lotusClientsLock.Lock()
			checkLotusEpoch()
			lotusClientsLock.Unlock()
			<-timer.C
		}
	}()

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
	return ln, nil
}

func handleLotus(srcConn net.Conn) {
	lotusClientsLock.Lock()
	defer lotusClientsLock.Unlock()
	if bestClient == nil {
		checkLotusEpoch()
	}
	client := bestClient
	if client == nil {
		srcConn.Close()
		return
	}

	_, targetConn, err := client.GetConn()
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
			srcConn.Close()
			targetConn.Close()
		}
	}()

	// copy response.
	go func() {
		if _, err := io.Copy(targetConn, srcConn); err != nil {
			log.Warn(errors.As(err))
			srcConn.Close()
			targetConn.Close()
		}
	}()
}

func LoadLotusProxy(ctx context.Context, cfgFile string) error {
	lotusClientsLock.Lock()
	defer lotusClientsLock.Unlock()
	return loadLotusProxy(ctx, cfgFile)
}

func RealoadLotusProxy(ctx context.Context) error {
	lotusClientsLock.Lock()
	defer lotusClientsLock.Unlock()
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

	for i := len(records) - 1; i > 0; i-- {
		if len(records[i]) != 1 {
			return errors.New("no data or error format").As(records[i])
		}
		RegisterLotus(ctx, strings.TrimSpace(records[i][0]))
	}

	// checksum the token
	if len(lotusClients) == 0 {
		return errors.New("client not found")
	}
	// TODO: support different token.
	proxyAddr := cliutil.ParseApiInfo(strings.TrimSpace(records[0][0]))
	token := string(proxyAddr.Token)
	for i := len(lotusClients) - 1; i > 0; i-- {
		if token != string(lotusClients[i].apiInfo.Token) {
			return errors.New("tokens are not same").As(lotusClients[i].apiInfo.Addr)
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
	lotusClientsLock.Lock()
	defer lotusClientsLock.Unlock()
	result := []api.ProxyStatus{}
	for _, c := range lotusClients {
		result = append(result, api.ProxyStatus{
			Addr:   c.apiInfo.Addr,
			Alive:  c.IsAlive(),
			Height: c.curHeight,
		})
	}
	return result
}

func GetLotusProxy() *cliutil.APIInfo {
	lotusClientsLock.Lock()
	defer lotusClientsLock.Unlock()
	return lotusProxyAddr
}
