package impl

import (
	"context"

	"github.com/filecoin-project/go-lotus/api"
	"github.com/filecoin-project/go-lotus/chain/address"
	"github.com/filecoin-project/go-lotus/miner"
	"github.com/filecoin-project/go-lotus/node/impl/full"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("node")

type API struct {
	CommonAPI
	full.ChainAPI
	full.ClientAPI
	full.MpoolAPI
	full.PaychAPI
	full.StateAPI
	full.WalletAPI

	Miner *miner.Miner
}

func (a *API) MinerRegister(ctx context.Context, addr address.Address) error {
	return a.Miner.Register(addr)
}

var _ api.FullNode = &API{}
