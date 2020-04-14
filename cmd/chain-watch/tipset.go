package main

import (
	"context"
	"encoding/json"

	//"fmt"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	//kafka "github.com/filecoin-project/lotus/api"
)

var topic_report = "browser"

type blockInfos struct {
	Type         string
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

	/*	cids := ts.Cids()
		blks := ts.Blocks()
		height := ts.Height
		for i := 0; i < len(blks); i++ {
			cid := cids[i]
			blk := blks[i]
			blockInfos := blockInfos{}
			blockInfos.Type = "block"
			cidJson, _ := json.Marshal(cid)
			blockInfos.BlockId = string(cidJson)
			blockInfos.BlockHeight = fmt.Sprintf("%d", height)
			blockInfos.BlockSize = "0"
			blockInfos.BlockHash = string(cidJson)
			miner, _ := json.Marshal(blk.Miner)
			blockInfos.MinerCode = string(miner)
			blockInfos.BlockTime = fmt.Sprintf("%d", blk.Timestamp)
			blockInfos.Reward = "0"
			ticker, _ := json.Marshal(blk.Ticket)
			blockInfos.Ticketing = string(ticker)
			parentStateRoot, _ := json.Marshal(blk.ParentStateRoot)
			blockInfos.TreeRoot = string(parentStateRoot)
			blockSig, _ := json.Marshal(blk.BlockSig)
			blockInfos.Autograph = string(blockSig)
			parents, _ := json.Marshal(blk.Parents)
			blockInfos.Parents = string(parents)
			parentWeight, _ := json.Marshal(blk.ParentWeight)
			blockInfos.ParentWeight = string(parentWeight)
			blockInfos.MessageNum = "0"
			blockInfos.MinerAddress = "xxxxx"
			bi, _ := json.Marshal(blockInfos)
			//kafka.KafkaProducer(string(bi), topic_report)
			log.Info("block消息结构==: ", string(bi))

		}
	*/
	log.Infof("Getting synced block list:%s", string(tsData))
	// TODO: send tipset to kafka
}
