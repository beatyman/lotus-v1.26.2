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

type blockInfo struct {
	BlockHeight           string
	BlockSize             interface{}
	BlockHash             interface{}
	BlockTime             string
	MinerCode             interface{}
	Reward                interface{}
	Ticketing             interface{}
	TreeRoot              interface{}
	Autograph             interface{}
	Parents               interface{}
	ParentWeight          interface{}
	MessageNum            interface{}
	Ticket                string
	TransactionSpend      string
	BlsMessages           interface{}
	SecpkMessages         interface{}
	EPostProof            interface{}
	ParentMessageReceipts interface{}
	Messages              interface{}
	BLSAggregate          interface{}
	ForkSignaling         interface{}
}

type blocks struct {
	//Type       string
	KafkaCommon
	BlockInfos []blockInfo
	PledgeNum  string
	MinTicket  interface{}
}

func SerialJson(obj interface{}) string {
	out, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return string(out)
}

func syncHead(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet, maxBatch int) {

	pledgeNum, _ := api.StatePledgeCollateral(ctx, ts.Key())
	tsData := SerialJson(ts)
	_ = tsData
	log.Infof("Getting synced block list:%s", string(tsData))

	minTicketBlock := ts.MinTicketBlock()
	log.Infof("minTicketBlock:%s", minTicketBlock.Cid())

	cids := ts.Cids()
	blks := ts.Blocks()

	blockInfos := []blockInfo{}
	height := ts.Height()
	for i := 0; i < len(blks); i++ {
		cid := cids[i]
		blk := blks[i]
		blockMessages, err := api.ChainGetBlockMessages(ctx, cid)
		log.Info("ChainGetBlockMessages:", SerialJson(blockMessages))
		if err != nil {
			log.Error(err)
			continue
		}
		blockInfo := blockInfo{}
		blockInfo.BlockHeight = fmt.Sprintf("%d", height)
		readObj, err := api.ChainReadObj(ctx, cid)
		if err != nil {
			log.Error(err)
			continue
		}
		blockInfo.BlockSize = len(readObj)

		/*log.Infof("readObj :%s", readObj)
		block, err := blk.ToStorageBlock()
		if err != nil {
			log.Error(err)
			continue
		}
		rawData := block.RawData()
		if err != nil {
			log.Error(err)
			continue
		}
		log.Infof("block len:%d, block:%s", len(rawData), rawData)
		*/

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
		blockInfo.BlsMessages = blockMessages.BlsMessages
		blockInfo.SecpkMessages = blockMessages.SecpkMessages
		blockInfo.EPostProof = blk.ElectionProof
		blockInfo.ParentMessageReceipts = blk.ParentMessageReceipts
		blockInfo.Messages = blk.Messages
		blockInfo.BLSAggregate = blk.BLSAggregate
		blockInfo.ForkSignaling = blk.ForkSignaling
		if i == 0 {
			blockInfo.Ticket = "1"
		} else {
			blockInfo.Ticket = "0"
		}
		blockInfo.TransactionSpend = "0"
		//bk := SerialJson(blockInfo)
		//KafkaProducer(bk, topic_report)
		//log.Info("block消息结构==: ", string(bk))
		blockInfos = append(blockInfos, blockInfo)
	}

	blocks := blocks{
		KafkaCommon: KafkaCommon{
			KafkaId:        GenKID(),
			KafkaTimestamp: GenKTimestamp(),
			Type:           "block",
		},
	}
	//blocks.Type = "block"
	blocks.BlockInfos = blockInfos
	blocks.PledgeNum = fmt.Sprintf("%d", pledgeNum)
	blocks.MinTicket = minTicketBlock.Cid()
	bjson := SerialJson(blocks)
	if err := KafkaProducer(bjson, _kafkaTopic); err != nil {
		log.Error(err)
		return
	}
	log.Info("blocks message send##: ", string(bjson))
}
