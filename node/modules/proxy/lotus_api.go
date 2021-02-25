package proxy

import (
	"context"
	"time"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/gwaylib/errors"
)

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

func UseLotusDefault(ctx context.Context, addr cliutil.APIInfo) error {
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
	changeLotusNode(idx)
	return nil
}

// for lotus api connect
// if the proxy on, it would be miner's proxy.
// else it should be the origin api connection.
func LotusProxyAddr() *cliutil.APIInfo {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	return lotusProxyAddr
}

func LotusProxyStatus(ctx context.Context, cond api.ProxyStatCondition) (*api.ProxyStatus, error) {
	lotusNodesLock.Lock()
	defer lotusNodesLock.Unlock()
	checkLotusEpoch()
	return lotusProxyStatus(ctx, cond)
}
