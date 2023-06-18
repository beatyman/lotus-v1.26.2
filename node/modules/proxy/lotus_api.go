package proxy

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/cli/util/apiaddr"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/gwaylib/errors"
)

const (
	PROXY_FILE = "lotus.proxy"
)

var (
	PROXY_AUTO = datastore.NewKey("lotus.proxy.auto")
)

type NetConnect func(context.Context, peer.AddrInfo) error

func CreateLotusProxyFile(lotusRepo string) error {
	proxyFile := filepath.Join(lotusRepo, PROXY_FILE)
	if _, err := os.Stat(proxyFile); err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err)
		}
		// proxy file not exist, create it.
		token, err := ioutil.ReadFile(filepath.Join(lotusRepo, "token"))
		if err != nil {
			return errors.As(err)
		}
		api, err := ioutil.ReadFile(filepath.Join(lotusRepo, "api"))
		if err != nil {
			return errors.As(err)
		}
		// is the next line '\n' or '\r\n'
		output := fmt.Sprintf(`# The first line is for proxy addr
%s:/ip4/127.0.0.1/tcp/0/http
# When there are multiple nodes, separate each node in line
%s:%s`, string(token), string(token), string(api))
		if err := ioutil.WriteFile(proxyFile, []byte(output), 0600); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

func UseLotusProxy(ctx context.Context, cfgPath string, defAddr apiaddr.APIInfo) error {
	lotusNodesLk.Lock()
	defer lotusNodesLk.Unlock()
	lotusProxyOn = true
	if err := loadLotusProxy(ctx, cfgPath); err != nil {
		lotusProxyOn = false
		defLotusNode = &LotusNode{ctx: ctx, apiInfo: defAddr}
		if err := reloadNodes(&defAddr, []*LotusNode{
			&LotusNode{
				ctx:     ctx,
				apiInfo: defAddr,
			},
		}); err != nil {
			return errors.As(err)
		}
	} else {
		// only once call.
		go func() {
			lotusCheckOnce.Do(func() {
				tick := time.Tick(time.Duration(build.BlockDelaySecs) * time.Second)
				for {
					lotusNodesLk.Lock()
					checkLotusEpoch()
					lotusNodesLk.Unlock()
					<-tick
				}
			})
		}()

	}
	return nil
}

func RealoadLotusProxy(ctx context.Context) error {
	lotusNodesLk.Lock()
	defer lotusNodesLk.Unlock()

	if len(lotusProxyCfg) == 0 {
		return errors.New("no proxy in running")
	}

	if !lotusProxyOn {
		return errors.New("proxy not on, need restart the deployment to lotus.proxy mode")
	}
	return loadLotusProxy(ctx, lotusProxyCfg)
}

func SetLotusAutoSelect(on bool) error {
	lotusNodesLk.Lock()
	defer lotusNodesLk.Unlock()
	if !lotusProxyOn {
		return errors.New("lotux proxy not on")
	}
	lotusAutoSelect = on
	return nil
}

func LotusProxyChange(idx int) error {
	lotusNodesLk.Lock()
	defer lotusNodesLk.Unlock()
	if !lotusProxyOn {
		return errors.New("lotux proxy not on")
	}
	return changeLotusNode(idx)
}

// for lotus api connect
// if the proxy on, it would be miner's proxy.
// else it should be the origin api connection.
func LotusProxyAddr() *apiaddr.APIInfo {
	lotusNodesLk.RLock()
	defer lotusNodesLk.RUnlock()
	return lotusProxyAddr
}

func LotusProxyNetConnect(mp2p NetConnect) (bool, error) {
	lotusNodesLk.RLock()
	defer lotusNodesLk.RUnlock()
	minerp2p = mp2p
	connected := false
	for _, node := range lotusNodes {
		if !node.IsAlive() {
			continue
		}
		apiConn, err := node.GetNodeApiV1(_NODE_ALIVE_CONN_KEY)
		if err != nil {
			return connected, errors.As(err)
		}
		nApi := apiConn.NodeApi
		remoteAddrs, err := nApi.NetAddrsListen(node.ctx)
		if err != nil {
			return connected, errors.As(err)
		}
		log.Infof("minerp2p connect to : %s", remoteAddrs)
		if err := minerp2p(node.ctx, remoteAddrs); err != nil {
			return connected, errors.As(err)
		}
		connected = true
	}
	return connected, nil
}

func LotusProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	lotusNodesLk.Lock()
	defer lotusNodesLk.Unlock()
	checkLotusEpoch()
	return lotusProxyStatus(ctx, cond)
}
