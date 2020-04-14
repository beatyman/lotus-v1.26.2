package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"

	aapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
)

type Message struct {
	types.Message
	Type      string
	OriParams interface{}
}

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

		msgs := map[cid.Cid]*Message{}
		for _, v := range updates {
			if v.Type != aapi.MpoolAdd {
				continue
			}
			msg := &Message{
				Message:   v.Message.Message,
				Type:      "message",
				OriParams: map[string]string{},
			}

			to := fmt.Sprintf("%s", msg.To)
			switch {
			// builtint.MethosPower
			case strings.HasPrefix(to, "t04"):
				switch msg.Method {
				case builtin.MethodsPower.CreateMiner:
					var params power.CreateMinerParams
					if err := params.UnmarshalCBOR(bytes.NewReader(msg.Params)); err != nil {
						log.Error(err)
						break
					}
					msg.OriParams = params
				}

				// TODO: decode more actors
			case strings.HasPrefix(to, "t01"):
				switch msg.Method {
				// 钱包转账
				case builtin.MethodSend:
					// 没有参数可以解析
				case builtin.MethodsMiner.SubmitWindowedPoSt:
					var params abi.OnChainPoStVerifyInfo
					if err := params.UnmarshalCBOR(bytes.NewReader(msg.Params)); err != nil {
						log.Error(err)
						break
					}
					msg.OriParams = params

				case builtin.MethodsMiner.PreCommitSector:
					var params miner.SectorPreCommitInfo
					if err := params.UnmarshalCBOR(bytes.NewReader(msg.Params)); err != nil {
						log.Error(err)
						break
					}
					msg.OriParams = params
				case builtin.MethodsMiner.ProveCommitSector:
					// 存力提交
					var params miner.ProveCommitSectorParams
					if err := params.UnmarshalCBOR(bytes.NewReader(msg.Params)); err != nil {
						log.Error(err)
						break
					}
					msg.OriParams = params
				}
			}

			cid := v.Message.Message.Cid()
			msgs[cid] = msg

			// TODO: send to kafka
			mdata, err := json.Marshal(msg)
			if err != nil {
				log.Warn(errors.As(err))
				continue
			}
			if err := KafkaProducer(string(mdata), topic_report); err != nil {
				log.Warn(errors.As(err))
				continue
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
