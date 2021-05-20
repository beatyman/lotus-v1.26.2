package cliutil

import (
	"path/filepath"

	"github.com/filecoin-project/lotus/node/modules/proxy"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/gwaylib/errors"
)

func UseLotusProxy(ctx *cli.Context) error {
	repoFlag := FlagForRepo(repo.FullNode)
	p, err := homedir.Expand(ctx.String(repoFlag))
	if err != nil {
		return errors.As(err, repoFlag)
	}
	proxyFile := filepath.Join(p, proxy.PROXY_FILE)
	return proxy.UseLotusProxy(ctx.Context, proxyFile)
}

func GetAPIInfo(ctx *cli.Context, t repo.RepoType) (APIInfo, error) {
	return proxyAPIInfo(ctx, t)
}

func proxyAPIInfo(ctx *cli.Context, t repo.RepoType) (APIInfo, error) {
	// hlm implement start
	switch t {
	case repo.FullNode:
		proxyAddr := proxy.LotusProxyAddr()
		if proxyAddr != nil {
			return *proxyAddr, nil
		}

		// log.Info("Get proxy api failed, using the default lotus api")

		// using default
		addr, err := getAPIInfo(ctx, t)
		if err == nil {
			proxy.UseLotusDefault(ctx.Context, addr)
		}
		return addr, err
	default:
		return getAPIInfo(ctx, t)
	}
	// hlm implement end
}
