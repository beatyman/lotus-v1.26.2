package main

import (
	"context"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
)

func runSyncer(ctx context.Context, api api.FullNode, st io.Writer, maxBatch int) {
	notifs, err := api.ChainNotify(ctx)
	if err != nil {
		panic(err)
	}
	go func() {
		for notif := range notifs {
			for _, change := range notif {
				switch change.Type {
				case store.HCCurrent:
					fallthrough
				case store.HCApply:
					go syncHead(ctx, api, st, change.Val, maxBatch)
				case store.HCRevert:
					log.Warnf("revert todo")
				}
				log.Info("=====message======", change.Type, ":", store.HCCurrent)
				if change.Type == store.HCCurrent {
					go subMpool(ctx, api, st, change.Val)
					// go subMpool(ctx, api, st, change.Val)
					// go subBlocks(ctx, api, st)
					// go subInfo(ctx, api, st, change.Val)
				}
			}
		}
	}()
}

func subInfo(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet) {
	subMpool(ctx, api, st, ts)
	subBlocks(ctx, api, st)
}
