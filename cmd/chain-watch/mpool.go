package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	aapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/filecoin-project/specs-actors/actors/runtime/exitcode"
)

type MessageReceipt struct {
	Height   abi.ChainEpoch
	BlockCid string
	ExitCode exitcode.ExitCode
	Return   []byte
	GasUsed  int64
}
type Message struct {
	KafkaCommon
	types.Message
	Cid       string
	Size      int
	OriParams interface{}
	Receipt   MessageReceipt
	ToActor   map[string]interface{}
	FromActor map[string]interface{}
}

func minerInfo(ctx context.Context, api aapi.FullNode, addr address.Address) (map[string]interface{}, error) {
	// 获取矿工存力数据
	pow, err := api.StateMinerPower(ctx, addr, types.EmptyTSK)
	if err != nil {
		log.Error(err)
		// Not sure why this would fail, but its probably worth continuing
	}

	// 获取矿工节点信息
	mInfo, err := api.StateMinerInfo(ctx, addr, types.EmptyTSK)
	if err != nil {
		return nil, errors.As(err)
	}

	// 获取失败的扇区数
	sectorFaults, err := api.StateMinerFaults(ctx, addr, types.EmptyTSK)
	if err != nil {
		return nil, errors.As(err)
	}
	return map[string]interface{}{
		"TotalPower": fmt.Sprint(pow.TotalPower.RawBytePower),
		"MinerPower": fmt.Sprint(pow.MinerPower.RawBytePower),

		"PeerID": mInfo.PeerId.String(),
		"Owner":  fmt.Sprint(mInfo.Owner),
		"Worker": fmt.Sprint(mInfo.Worker),

		"SectorSize":  mInfo.SectorSize.String(),
		"FaultNumber": strconv.Itoa(len(sectorFaults)),
	}, nil
}

var (
	waitMsgLock = sync.Mutex{}
)

func getReceipt(ctx context.Context, api aapi.FullNode, cid cid.Cid) (*aapi.MsgLookup, error) {
	waitMsgLock.Lock()
	defer waitMsgLock.Unlock()
	return api.StateWaitMsg(ctx, cid)
}

func subMpool(ctx context.Context, api aapi.FullNode, storage io.Writer, curTs *types.TipSet) {
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
			log.Info("message in", v.Message.Message)
			cid := v.Message.Message.Cid()
			// 获取消息长度
			readObj, err := api.ChainReadObj(ctx, cid)
			if err != nil {
				log.Error(err)
				continue
			}
			log.Info("readObj done")
			// 获取收据
			rece, err := getReceipt(ctx, api, cid)
			if err != nil {
				log.Error(err)
				continue
			}
			receipt := MessageReceipt{}
			if rece != nil {
				receipt.Height = rece.TipSet.Height()
				receipt.ExitCode = rece.Receipt.ExitCode
				receipt.Return = rece.Receipt.Return
				receipt.GasUsed = rece.Receipt.GasUsed

				// checkout message from block
				cids := rece.TipSet.Cids()
			blkLoop:
				for _, cid := range cids {
					msgs, err := api.ChainGetBlockMessages(ctx, cid)
					if err != nil {
						log.Warn(errors.As(err))
						continue
					}
					for _, mCid := range msgs.Cids {
						if mCid.Equals(cid) {
							receipt.BlockCid = cid.String()
							break blkLoop
						}
					}
				}
			}
			log.Info("receipt done")
			ts := rece.TipSet
			// 获取帐户信息
			toStateActor, err := api.StateGetActor(ctx, v.Message.Message.To, ts.Key())
			if err != nil {
				log.Error(err)
				continue
			}
			log.Info("toStateActor done")
			toActorType := "Account"
			toActorMiner := map[string]interface{}{}
			if strings.HasPrefix(fmt.Sprint(v.Message.Message.To), "t0") && len(fmt.Sprint(v.Message.Message.To)) > 2 {
				toActorType = "StorageMiner"
				mInfo, err := minerInfo(ctx, api, v.Message.Message.To)
				if err != nil {
					log.Error(err)
					continue
				}
				toActorMiner = mInfo
				log.Info("toStateMiner done")
			}
			fromStateActor, err := api.StateGetActor(ctx, v.Message.Message.From, ts.Key())
			if err != nil {
				log.Error(err)
				continue
			}
			fromActorType := "Account"
			fromActorMiner := map[string]interface{}{}
			if strings.HasPrefix(fmt.Sprint(v.Message.Message.From), "t0") && len(fmt.Sprint(v.Message.Message.From)) > 2 {
				toActorType = "StorageMiner"
				mInfo, err := minerInfo(ctx, api, v.Message.Message.From)
				if err != nil {
					log.Error(err)
					continue
				}
				fromActorMiner = mInfo
			}
			toAct, err := api.StateLookupID(ctx, v.Message.Message.To, ts.Key())
			if err != nil {
				log.Error(err)
				continue
			}
			fromAct, err := api.StateLookupID(ctx, v.Message.Message.From, ts.Key())
			if err != nil {
				log.Error(err)
				continue
			}
			msg := &Message{
				KafkaCommon: KafkaCommon{
					KafkaId:        GenKID(),
					KafkaTimestamp: GenKTimestamp(),
					Type:           "message",
				},

				Message:   v.Message.Message,
				Cid:       cid.String(),
				Size:      len(readObj),
				OriParams: map[string]interface{}{},
				Receipt:   receipt,
				ToActor: map[string]interface{}{
					"Type": toActorType,

					// for actor struct
					"Actor": map[string]interface{}{
						"Code":    toStateActor.Code.String(),
						"Head":    toStateActor.Head.String(),
						"Nonce":   toStateActor.Nonce,
						"Balance": toStateActor.Balance.String(),
						"Act":     toAct.String(),
					},

					// for storage miner
					"Miner": toActorMiner,
				},
				FromActor: map[string]interface{}{
					"Type": fromActorType,

					// for actor struct
					"Actor": map[string]interface{}{
						"Code":    fromStateActor.Code.String(),
						"Head":    fromStateActor.Head.String(),
						"Nonce":   fromStateActor.Nonce,
						"Balance": fromStateActor.Balance.String(),
						"Act":     fromAct.String(),
					},

					// for storage miner
					"Miner": fromActorMiner,
				},
			}
			log.Info("getting message")
			msgs[cid] = msg

			to := fmt.Sprintf("%s", msg.To)
			switch {
			case msg.Method == 0:
				msg.OriParams = map[string]string{}
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
			default:
				switch msg.Method {
				case builtin.MethodsMiner.SubmitWindowedPoSt:
					var params miner.SubmitWindowedPoStParams
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

					msg.OriParams = map[string]interface{}{
						"SectorNumber": params.SectorNumber,
						"Proof":        params.Proof,
					}
				}
			}

			mdata, err := json.Marshal(msg)
			if err != nil {
				log.Warn(errors.As(err))
				continue
			}
			log.Info(string(mdata))
			// send to kafka
			KafkaProducer(string(mdata), _kafkaTopic)
		}

		log.Infof("Processing %d mpool updates", len(msgs))
	}
}
