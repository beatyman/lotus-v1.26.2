package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gwaylib/errors"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	aapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	miner2 "github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/filecoin-project/specs-actors/actors/runtime/exitcode"
)

type MessageReceipt struct {
	Height   abi.ChainEpoch
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
}

func minerInfo(ctx context.Context, api aapi.FullNode, addr address.Address, ts *types.TipSet) (miner2.State, error) {
	act, err := api.StateGetActor(ctx, addr, ts.Key())
	if err != nil {
		return miner2.State{}, err
	}
	aso, err := api.ChainReadObj(ctx, act.Head)
	if err != nil {
		return miner2.State{}, err
	}
	var mst miner2.State
	if err1 := mst.UnmarshalCBOR(bytes.NewReader(aso)); err1 != nil {
		return miner2.State{}, err
	}
	return mst, nil
}

func subMpool(ctx context.Context, api aapi.FullNode, storage io.Writer, ts *types.TipSet) {
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
			cid := v.Message.Message.Cid()
			// 获取消息长度
			readObj, err := api.ChainReadObj(ctx, cid)
			if err != nil {
				log.Error(err)
				continue
			}
			// 获取收据
			receipt, err := api.StateWaitMsg(ctx, cid)
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
				Receipt: MessageReceipt{
					Height:   receipt.TipSet.Height(),
					ExitCode: receipt.Receipt.ExitCode,
					Return:   receipt.Receipt.Return,
					GasUsed:  receipt.Receipt.GasUsed,
				},
			}
			msgs[cid] = msg

			to := fmt.Sprintf("%s", msg.To)
			switch {
			case msg.Method == 0:
				// 余额流转
				// 获取矿工节点信息
				toBalance, err := api.WalletBalance(ctx, msg.To)
				if err != nil {
					log.Error(err)
				}
				fromBalance, err := api.WalletBalance(ctx, msg.From)
				if err != nil {
					log.Error(err)
				}
				//log.Info("############", msg.To.String(), "::", msg.From)
				toAct, err := api.StateLookupID(ctx, msg.To, ts.Key())
				toStr := toAct.String()
				if err != nil {
					toStr = ""
					log.Error(err)
				}
				log.Info("#######", SerialJson(toAct))
				fromAct, err := api.StateLookupID(ctx, msg.From, ts.Key())
				fromStr := fromAct.String()
				if err != nil {
					fromStr = ""
					log.Error(err)
				}
				log.Info("#######", SerialJson(fromAct))

				secCounts, err := api.StateMinerSectorCount(ctx, msg.To, ts.Key())
				if err != nil {
					log.Error(err)
					break
				}
				log.Info("=========111===========", SerialJson(secCounts))
				minerTo, err := minerInfo(ctx, api, msg.To, ts)
				if err != nil {
					log.Error(err)
					break
				} else {
					log.Info("====================", SerialJson(minerTo))
				}
				minerFrom, err := minerInfo(ctx, api, msg.From, ts)
				if err != nil {
					log.Error(err)
					break
				} else {
					log.Info("====================", SerialJson(minerFrom))
				}
				msg.OriParams = map[string]string{
					"ToBalance":      toBalance.String(),
					"FromBalance":    fromBalance.String(),
					"toAct":          toStr,
					"fromAct":        fromStr,
					"toMinerOwner":   minerTo.Info.Owner.String(),
					"fromMinerOwner": minerFrom.Info.Owner.String(),
				}
			case strings.HasPrefix(to, "t04"):
				switch msg.Method {
				case builtin.MethodsPower.CreateMiner:
					var params power.CreateMinerParams
					if err := params.UnmarshalCBOR(bytes.NewReader(msg.Params)); err != nil {
						log.Error(err)
						break
					}
					log.Info("============OriParams==================", SerialJson(params))
					msg.OriParams = params
				}

				// TODO: decode more actors
			case strings.HasPrefix(to, "t01"):
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

					// 获取矿工存力数据
					pow, err := api.StateMinerPower(ctx, msg.To, types.EmptyTSK)
					if err != nil {
						log.Error(err)
						// Not sure why this would fail, but its probably worth continuing
					}

					// 获取矿工节点信息
					mInfo, err := api.StateMinerInfo(ctx, msg.To, types.EmptyTSK)
					if err != nil {
						log.Error(err)
						continue
					}

					// 获取失败的扇区数
					sectorFaults, err := api.StateMinerFaults(ctx, msg.To, types.EmptyTSK)
					if err != nil {
						log.Error(err)
						continue
					}
					msg.OriParams = map[string]interface{}{
						"TotalPower": fmt.Sprint(pow.TotalPower),
						"MinerPower": fmt.Sprint(pow.MinerPower),

						"PeerID": mInfo.PeerId.String(),
						"Owner":  fmt.Sprint(mInfo.Owner),
						"Worker": fmt.Sprint(mInfo.Worker),

						"SectorSize":   mInfo.SectorSize.String(),
						"FaultNumber":  strconv.Itoa(len(sectorFaults)),
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
			// send to kafka
			KafkaProducer(string(mdata), _kafkaTopic)
		}

		log.Infof("Processing %d mpool updates", len(msgs))
	}
}
