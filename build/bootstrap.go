package build

import (
	"context"
	"io/ioutil"
	"os"
	"strings"

	"github.com/filecoin-project/lotus/lib/addrutil"

	rice "github.com/GeertJohan/go.rice"
	"github.com/libp2p/go-libp2p-core/peer"
)

func BuiltinBootstrap() ([]peer.AddrInfo, error) {
	if DisableBuiltinAssets {
		return nil, nil
	}

	var out []peer.AddrInfo

	b := rice.MustFindBox("bootstrap")

	if BootstrappersFile != "" {
		spi := b.MustString(BootstrappersFile)
		if spi == "" {
			return nil, nil
		}
		pi, err := addrutil.ParseAddresses(context.TODO(), strings.Split(strings.TrimSpace(spi), "\n"))
		if err != nil {
			log.Warn(err)
		} else {
			out = append(out, pi...)
		}
	}

	// TODO:fetch from fivestar chains server
	if data, err := ioutil.ReadFile("/etc/lotus/bootstrap.pi"); err != nil {
		if !os.IsNotExist(err) {
			log.Warn(err)
		}
	} else {
		if pi, err := addrutil.ParseAddresses(context.TODO(), strings.Split(strings.TrimSpace(string(data)), "\n")); err != nil {
			log.Warn(err)
		} else {
			out = append(out, pi...)
		}
	}

	return out, nil
}
