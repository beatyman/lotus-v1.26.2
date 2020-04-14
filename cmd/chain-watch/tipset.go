package main

import (
	"context"
	"encoding/json"

	"fmt"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var topic_report = "browser"

type blockInfos struct {
	Type         string
	BlockId      interface{}
	BlockHeight  string
	BlockSize    interface{}
	BlockHash    interface{}
	BlockTime    string
	MinerCode    interface{}
	Reward       interface{}
	Ticketing    interface{}
	TreeRoot     interface{}
	Autograph    interface{}
	Parents      interface{}
	ParentWeight interface{}
	MessageNum   interface{}
	//MinerAddress string
	Ticket    string
	PledgeNum string
}

func SerialJson(obj interface{}) string {
	out, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func syncHead(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet, maxBatch int) {
	tsData, err := json.Marshal(ts)
	if err != nil {
		panic(err)
	}

	log.Info("st:==", SerialJson(st))

	pledgeNum, _ := api.StatePledgeCollateral(ctx, ts.Key())
	log.Info("TipSet:==", SerialJson(ts))

	cids := ts.Cids()
	blks := ts.Blocks()
	height := ts.Height
	for i := 0; i < len(blks); i++ {
		cid := cids[i]
		blk := blks[i]
		blockMessages, _ := api.ChainGetBlockMessages(ctx, cid)
		blockInfos := blockInfos{}
		blockInfos.Type = "block"
		blockInfos.BlockId = cid
		blockInfos.BlockHeight = fmt.Sprintf("%d", height)
		readObj, _ := api.ChainReadObj(ctx, cid)
		blockInfos.BlockSize = len(readObj)
		blockInfos.BlockHash = cid
		blockInfos.MinerCode = blk.Miner
		blockInfos.BlockTime = fmt.Sprintf("%d", blk.Timestamp)
		blockInfos.Reward = "0"
		blockInfos.Ticketing = blk.Ticket
		blockInfos.TreeRoot = blk.ParentStateRoot
		blockInfos.Autograph = blk.BlockSig
		blockInfos.Parents = blk.Parents
		blockInfos.ParentWeight = blk.ParentWeight
		blockInfos.MessageNum = len(blockMessages.BlsMessages) + len(blockMessages.SecpkMessages)
		//blockInfos.MinerAddress = "xxxxx"
		if i == 0 {
			blockInfos.Ticket = "1"
		} else {
			blockInfos.Ticket = "0"
		}
		blockInfos.PledgeNum = fmt.Sprintf("%d", pledgeNum)
		bk := SerialJson(blockInfos)
		KafkaProducer(bk, topic_report)
		log.Info("block消息结构==: ", string(bk))

	}

	log.Infof("Getting synced block list:%s", string(tsData))
	// TODO: send tipset to kafka
}
