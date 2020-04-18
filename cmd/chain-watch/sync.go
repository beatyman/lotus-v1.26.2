package main

import (
	"context"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/store"
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
					syncHead(ctx, api, st, change.Val, maxBatch)
				case store.HCRevert:
					log.Warnf("revert todo")
				}

				if change.Type == store.HCCurrent {
					go subMpool(ctx, api, st)
					go subBlocks(ctx, api, st)
				}
			}
		}
	}()
}
