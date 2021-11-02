package cliutil

import (
	"path/filepath"

	"github.com/filecoin-project/lotus/node/modules/proxy"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/gwaylib/errors"
)

func ConnectLotusProxy(ctx *cli.Context) error {
	repoFlag := FlagForRepo(repo.FullNode)
	p, err := homedir.Expand(ctx.String(repoFlag))
	if err != nil {
		return errors.As(err, repoFlag)
	}
	addr, err := GetAPIInfo(ctx, repo.FullNode)
	if err != nil {
		return errors.As(err)
	}
	proxyFile := filepath.Join(p, proxy.PROXY_FILE)
	return proxy.UseLotusProxy(ctx.Context, proxyFile, addr)
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
	}
	return getAPIInfo(ctx, t)
	// hlm implement end
}