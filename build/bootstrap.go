package build

import (
	"context"
	"embed"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/filecoin-project/lotus/lib/addrutil"
)

//go:embed bootstrap
var bootstrapfs embed.FS

func BuiltinBootstrap() ([]peer.AddrInfo, error) {
	if DisableBuiltinAssets {
		return nil, nil
	}

	var out []peer.AddrInfo
	if BootstrappersFile != "" {
		spi, err := bootstrapfs.ReadFile(path.Join("bootstrap", BootstrappersFile))
		if err != nil {
			return nil, err
		}
		if len(spi) == 0 {
			return nil, nil
		}
		pi, err := addrutil.ParseAddresses(context.TODO(), strings.Split(strings.TrimSpace(string(spi)), "\n"))
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
