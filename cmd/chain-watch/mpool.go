package main

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"

	aapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

func subMpool(ctx context.Context, api aapi.FullNode, storage io.Writer) {
	sub, err := api.MpoolSub(ctx)
	if err != nil {
		return
	}

	for {
		var updates []aapi.MpoolUpdate

		select {
		case update := <-sub:
			updates = append(updates, update)
		case <-ctx.Done():
			return
		}

	loop:
		for {
			time.Sleep(10 * time.Millisecond)
			select {
			case update := <-sub:
				updates = append(updates, update)
			default:
				break loop
			}
		}

		msgs := map[cid.Cid]*types.Message{}
		for _, v := range updates {
			if v.Type != aapi.MpoolAdd {
				continue
			}

			cid := v.Message.Message.Cid()
			msgs[cid] = &v.Message.Message
			mdata, err := json.Marshal(v.Message)
			if err != nil {
				log.Warn(errors.As(err))
			}
			log.Infof("mpool cid:%s,msg:%s", cid, string(mdata))
		}

		log.Infof("Processing %d mpool updates", len(msgs))

		// send to kafka
		// err := st.storeMessages(msgs)
		// if err != nil {
		// 	log.Error(err)
		// }
		//
		// if err := st.storeMpoolInclusions(updates); err != nil {
		// 	log.Error(err)
		// }
	}
}
