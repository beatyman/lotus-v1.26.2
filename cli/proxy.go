package cli

import (
	"path/filepath"

	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/node/modules/proxy"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/gwaylib/errors"
)

func UseLotusProxy(ctx *cli.Context) error {
	repoFlag := cliutil.FlagForRepo(repo.FullNode)
	p, err := homedir.Expand(ctx.String(repoFlag))
	if err != nil {
		return errors.As(err, repoFlag)
	}
	proxyFile := filepath.Join(p, "lotus.proxy")
	return proxy.UseLotusProxy(ctx.Context, proxyFile)
}

func GetAPIInfo(ctx *cli.Context, t repo.RepoType) (cliutil.APIInfo, error) {
	// hlm implement start
	switch t {
	case repo.FullNode:
		proxyAddr := proxy.LotusProxyAddr()
		if proxyAddr != nil {
			return *proxyAddr, nil
		}

		// using default
		addr, err := cliutil.GetAPIInfo(ctx, t)
		if err == nil {
			proxy.UseLotusDefault(ctx.Context, addr)
		}
		return addr, err
	default:
		return cliutil.GetAPIInfo(ctx, t)
	}
	// hlm implement end
}
