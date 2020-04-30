package main

import (
	"context"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/store"
)

func runSyncer(ctx context.Context, api api.FullNode) {
	// checking process
	log.Info("Get sync state")
	base, err := api.SyncState(ctx)
	if err != nil {
		log.Error(err)
		return
	}
	if len(base.ActiveSyncs) > 0 {
		log.Infof("Base sync state:%+v", base.ActiveSyncs[0].Base.Height())
		syncHead(ctx, api, base.ActiveSyncs[0].Base)
	}

	// listen change
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
					syncHead(ctx, api, change.Val)
				case store.HCRevert:
					log.Warnf("revert todo")
				}
				log.Info("=====message======", change.Type, ":", store.HCCurrent)
				if change.Type == store.HCCurrent {
					// go subMpool(ctx, api, st, change.Val)
					// go subBlocks(ctx, api, st)
				}
			}
		}
	}()
}
