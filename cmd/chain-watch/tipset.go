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

type blockInfo struct {
	//Type         string
	//BlockId      interface{}
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
	Ticket string
	//PledgeNum string
}

type blocks struct {
	Type       string
	BlockInfos []blockInfo
	PledgeNum  string
}

func SerialJson(obj interface{}) string {
	out, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func syncHead(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet, maxBatch int) {
	/*tsData, err := json.Marshal(ts)
	if err != nil {
		panic(err)
	}*/
	pledgeNum, _ := api.StatePledgeCollateral(ctx, ts.Key())

	cids := ts.Cids()
	blks := ts.Blocks()

	blockInfos := []blockInfo{}
	height := ts.Height
	for i := 0; i < len(blks); i++ {
		cid := cids[i]
		blk := blks[i]
		blockMessages, _ := api.ChainGetBlockMessages(ctx, cid)
		blockInfo := blockInfo{}
		//blockInfo.Type = "block"
		//blockInfo.BlockId = cid
		blockInfo.BlockHeight = fmt.Sprintf("%d", height)
		readObj, _ := api.ChainReadObj(ctx, cid)
		blockInfo.BlockSize = len(readObj)
		blockInfo.BlockHash = cid
		blockInfo.MinerCode = blk.Miner
		blockInfo.BlockTime = fmt.Sprintf("%d", blk.Timestamp)
		blockInfo.Reward = "0"
		blockInfo.Ticketing = blk.Ticket
		blockInfo.TreeRoot = blk.ParentStateRoot
		blockInfo.Autograph = blk.BlockSig
		blockInfo.Parents = blk.Parents
		blockInfo.ParentWeight = blk.ParentWeight
		blockInfo.MessageNum = len(blockMessages.BlsMessages) + len(blockMessages.SecpkMessages)
		//blockInfos.MinerAddress = "xxxxx"
		if i == 0 {
			blockInfo.Ticket = "1"
		} else {
			blockInfo.Ticket = "0"
		}
		//blockInfo.PledgeNum = fmt.Sprintf("%d", pledgeNum)
		bk := SerialJson(blockInfo)
		//KafkaProducer(bk, topic_report)
		log.Info("block消息结构==: ", string(bk))
		blockInfos = append(blockInfos, blockInfo)
	}

	blocks := blocks{}
	blocks.Type = "block"
	blocks.BlockInfos = blockInfos
	blocks.PledgeNum = fmt.Sprintf("%d", pledgeNum)
	bjson := SerialJson(blocks)
	KafkaProducer(bjson, topic_report)
	log.Info("blocks消息结构##: ", string(bjson))

	//log.Infof("Getting synced block list:%s", string(tsData))
	// TODO: send tipset to kafka
}
