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
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/gwaylib/errors"
)

const (
	PROXY_FILE = "lotus.proxy"
)

type NetConnect func(context.Context, peer.AddrInfo) error

func CreateLotusProxyFile(lotusRepo string) error {
	proxyFile := filepath.Join(lotusRepo, PROXY_FILE)
	//return proxy.UseLotusProxy(ctx.Context, proxyFile)
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
		output := fmt.Sprintf(`# the first line is for proxy addr
%s:/ip4/127.0.0.1/tcp/0/http
# bellow is the cluster node.
%s:%s`, string(token), string(token), string(api))
		if err := ioutil.WriteFile(proxyFile, []byte(output), 0600); err != nil {
			return errors.As(err)
		}
	}
	return nil
}

func UseLotusProxy(ctx context.Context, cfgFile string) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if err := loadLotusProxy(ctx, cfgFile); err != nil {
		return errors.As(err)
	}
	lotusProxyOn = true

	// only once call.
	go func() {
		lotusCheckOnce.Do(func() {
			tick := time.Tick(time.Duration(build.BlockDelaySecs) * time.Second)
			for {
				lotusNodesLock.Lock()
				checkLotusEpoch()
				lotusNodesLock.Unlock()
				<-tick
			}
		})
	}()
	return nil
}

func UseLotusDefault(ctx context.Context, addr apiaddr.APIInfo) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	lotusProxyAddr = &addr
	lotusProxyOn = false
	return reloadNodes(nil, []*LotusNode{
		&LotusNode{
			ctx:     ctx,
			apiInfo: addr,
		},
	})
}

func RealoadLotusProxy(ctx context.Context) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()

	if len(lotusProxyCfg) == 0 {
		return errors.New("no proxy in running")
	}

	if !lotusProxyOn {
		return errors.New("proxy not on, need restart the deployment to lotus.proxy mode")
	}
	return loadLotusProxy(ctx, lotusProxyCfg)
}

func SelectBestNode() error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if !lotusProxyOn {
		return errors.New("proxy not on")
	}
	selectBestNode(3)
	return nil
}

func SetLotusAutoSelect(on bool) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if !lotusProxyOn {
		return errors.New("lotux proxy not on")
	}
	lotusAutoSelect = on
	return nil
}

func LotusProxyChange(idx int) error {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	if !lotusProxyOn {
		return errors.New("lotux proxy not on")
	}
	return changeLotusNode(idx)
}

// for lotus api connect
// if the proxy on, it would be miner's proxy.
// else it should be the origin api connection.
func LotusProxyAddr() *apiaddr.APIInfo {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	return lotusProxyAddr
}

func LotusProxyNetConnect(mp2p NetConnect) (bool, error) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	minerp2p = mp2p
	proxyOn := false
	for _, node := range lotusNodes {
		if !node.IsAlive() {
			continue
		}
		nApi, err := node.getNodeApi()
		if err != nil {
			return proxyOn, errors.As(err)
		}
		remoteAddrs, err := nApi.NetAddrsListen(node.ctx)
		if err != nil {
			return proxyOn, errors.As(err)
		}
		log.Infof("minerp2p connect to : %s", remoteAddrs)
		if err := minerp2p(node.ctx, remoteAddrs); err != nil {
			return proxyOn, errors.As(err)
		}
		proxyOn = true
	}
	return proxyOn, nil
}

func LotusProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	checkLotusEpoch()
	return lotusProxyStatus(ctx, cond)
}
