package config

import (
	"encoding"
	"time"
)

// Common is common config between full node and miner
type Common struct {
	API    API
	Libp2p Libp2p
}

// FullNode is a full node config
type FullNode struct {
	Common
	Metrics Metrics
}

// StorageMiner is a storage miner config
type StorageMiner struct {
	Common
}

// API contains configs for API endpoint
type API struct {
	ListenAddress string
	Timeout       Duration
}

// Libp2p contains configs for libp2p
type Libp2p struct {
	ListenAddresses []string
	BootstrapPeers  []string
}

type Metrics struct {
	Nickname string
}

func defCommon() Common {
	return Common{
		API: API{
			ListenAddress: "/ip6/::1/tcp/1234/http",
			Timeout:       Duration(30 * time.Second),
		},
		Libp2p: Libp2p{
			ListenAddresses: []string{
				"/ip4/0.0.0.0/tcp/0",
				"/ip6/::/tcp/0",
			},
		},
	}

}

// Default returns the default config
func DefaultFullNode() *FullNode {
	return &FullNode{
		Common: defCommon(),
	}
}

func DefaultStorageMiner() *StorageMiner {
	return &StorageMiner{
		Common: defCommon(),
	}
}

var _ encoding.TextMarshaler = (*Duration)(nil)
var _ encoding.TextUnmarshaler = (*Duration)(nil)

// Duration is a wrapper type for time.Duration
// for decoding and encoding from/to TOML
type Duration time.Duration

// UnmarshalText implements interface for TOML decoding
func (dur *Duration) UnmarshalText(text []byte) error {
	d, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*dur = Duration(d)
	return err
}

func (dur Duration) MarshalText() ([]byte, error) {
	d := time.Duration(dur)
	return []byte(d.String()), nil
}
