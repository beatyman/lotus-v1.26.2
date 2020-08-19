package build

import (
	"context"
	"io/ioutil"
	"os"
	"strings"

	"github.com/filecoin-project/lotus/lib/addrutil"
	"golang.org/x/xerrors"

	rice "github.com/GeertJohan/go.rice"
	"github.com/libp2p/go-libp2p-core/peer"
)

func BuiltinBootstrap() ([]peer.AddrInfo, error) {
	if DisableBuiltinAssets {
		return nil, nil
	}

	var out []peer.AddrInfo
	// TODO:fetch from fivestar chains server
	if data, err := ioutil.ReadFile("/etc/lotus/boostrap.pi"); err != nil {
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

	b := rice.MustFindBox("bootstrap")
	err := b.Walk("", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return xerrors.Errorf("failed to walk box: %w", err)
		}

		if !strings.HasSuffix(path, ".pi") {
			return nil
		}
		spi := b.MustString(path)
		if spi == "" {
			return nil
		}
		pi, err := addrutil.ParseAddresses(context.TODO(), strings.Split(strings.TrimSpace(spi), "\n"))
		out = append(out, pi...)
		return err
	})

	return out, err
}
