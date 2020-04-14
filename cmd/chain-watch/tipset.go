package main

import (
	"context"
	"encoding/json"
	"io"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

func syncHead(ctx context.Context, api api.FullNode, st io.Writer, ts *types.TipSet, maxBatch int) {
	tsData, err := json.Marshal(ts)
	if err != nil {
		panic(err)
	}
	_ = tsData
	// log.Infof("Getting synced block list:%s", string(tsData))
	// TODO: send tipset to kafka
}
