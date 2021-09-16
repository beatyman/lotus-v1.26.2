package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

type rpcClient struct {
	api.WorkerHlmAPI
	closer jsonrpc.ClientCloser
}

func (r *rpcClient) Close() error {
	r.closer()
	return nil
}

func ConnectHlmWorker(ctx context.Context, fa api.HlmMinerSchedulerAPI, url string) (*rpcClient, error) {
	token, err := fa.AuthNew(ctx, []auth.Permission{"admin"}) // using the miner token for worker rpc server.
	if err != nil {
		return nil, errors.New("creating auth token for remote connection").As(err)
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))
	urlInfo := strings.Split(url, ":")
	if len(urlInfo) != 2 {
		panic("error url format")
	}
	rpcUrl := fmt.Sprintf("ws://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])

	wapi, closer, err := client.NewWorkerHlmRPC(ctx, rpcUrl, headers)
	if err != nil {
		return nil, errors.New("creating jsonrpc client").As(err)
	}

	return &rpcClient{wapi, closer}, nil
}

func CallCommit2Service(ctx context.Context, task ffiwrapper.WorkerTask, c1out storage.Commit1Out) (storage.Proof, error) {
	napi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}
	rCfg, err := napi.RetrySelectCommit2Service(ctx, task.SectorID)
	if err != nil {
		return nil, errors.As(err)
	}
	defer func() {
		if err := napi.RetryUnlockGPUService(ctx, rCfg.ID, task.Key()); err != nil {
			log.Warn(errors.As(err))
		}
	}()

	log.Infof("Selected Commit2 Service: %s", rCfg.SvcUri)

	// connect to remote worker
	rClient, err := ConnectHlmWorker(ctx, napi, rCfg.SvcUri)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rClient.Close()

	// do work
	return rClient.SealCommit2(ctx, api.SectorRef{SectorID: task.SectorID, ProofType: task.ProofType}, c1out)
}
