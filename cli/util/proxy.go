package cliutil

import (
	"fmt"
	"io/ioutil"
	"os"
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
	proxyFile := filepath.Join(p, "lotus.proxy")
	if _, err := os.Stat(proxyFile); err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err)
		}
		// proxy file not exist, create it.
		token, err := ioutil.ReadFile(filepath.Join(p, "token"))
		if err != nil {
			return errors.As(err)
		}
		api, err := ioutil.ReadFile(filepath.Join(p, "api"))
		if err != nil {
			return errors.As(err)
		}
		output := fmt.Sprintf(`
# the first line is for proxy addr
%s:/ip4/127.0.0.1/tcp/0/http
# bellow is the cluster node.
%s:%s
`, string(token), string(token), string(api))
		if err := ioutil.WriteFile(proxyFile, []byte(output), 0600); err != nil {
			return errors.As(err)
		}
	}
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
