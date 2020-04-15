package main

import (
	"context"
	"encoding/json"

	"fmt"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	_ "github.com/gwaylib/errors"
)

var topic_report = "browser"

type blockInfos struct {
	KafkaCommon
	BlockId      string
	BlockHeight  string
	BlockSize    string
	BlockHash    string
	BlockTime    string
	MinerCode    string
	Reward       string
	Ticketing    string
	TreeRoot     string
	Autograph    string
	Parents      string
	ParentWeight string
	MessageNum   string
	MinerAddress string
	Ticket       string
}

func syncHead(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet, maxBatch int) {
	tsData, err := json.Marshal(ts)
	if err != nil {
		panic(err)
	}
	_ = tsData
	// log.Infof("Getting synced block list:%s", string(tsData))

	cids := ts.Cids()
	blks := ts.Blocks()
	height := ts.Height
	for i := 0; i < len(blks); i++ {
		cid := cids[i]
		blk := blks[i]
		blockInfos := blockInfos{
			KafkaCommon: KafkaCommon{
				KafkaId:        GenKID(),
				KafkaTimestamp: GenKTimestamp(),
				Type:           "block",
			},
		}
		cidJson, _ := json.Marshal(cid)
		blockInfos.BlockId = string(cidJson)
		blockInfos.BlockHeight = fmt.Sprintf("%d", height)
		blockInfos.BlockSize = "0"
		blockInfos.BlockHash = string(cidJson)
		blockInfos.MinerCode = fmt.Sprint(blk.Miner)
		blockInfos.BlockTime = fmt.Sprintf("%d", blk.Timestamp)
		blockInfos.Reward = "0"
		ticker, _ := json.Marshal(blk.Ticket)
		blockInfos.Ticketing = string(ticker)
		blockInfos.TreeRoot = fmt.Sprint(blk.ParentStateRoot)
		blockSig, _ := json.Marshal(blk.BlockSig)
		blockInfos.Autograph = string(blockSig)
		parents, _ := json.Marshal(blk.Parents)
		blockInfos.Parents = string(parents)
		parentWeight, _ := json.Marshal(blk.ParentWeight)
		blockInfos.ParentWeight = string(parentWeight)
		blockInfos.MessageNum = "0"
		blockInfos.MinerAddress = "xxxxx"
		// bi, err := json.Marshal(blockInfos)
		// if err != nil {
		// 	log.Warn(errors.As(err))
		// 	continue
		// }
		// if err := KafkaProducer(string(bi), topic_report); err != nil {
		// 	log.Warn(errors.As(err))
		// 	continue
		// }
		// log.Infof("block消息结构==: %s", string(bi))
	}
}
