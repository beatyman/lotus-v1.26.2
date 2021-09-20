package main

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"net/http"
	"strings"
	"sync"
)

var (
	rpcClientApiRW = &sync.RWMutex{}
	rpcClientApi   *rpcClient
)

type rpcClient struct {
	api.WorkerHlmAPI
	closer jsonrpc.ClientCloser
}

func (r *rpcClient) RetryEnable(err error) bool {
	return err != nil &&
		(strings.Contains(err.Error(), "websocket connection closed") ||
			strings.Contains(err.Error(), "connection refused"))
}

func ConnectHlmWorker(ctx context.Context, fa *api.RetryHlmMinerSchedulerAPI, url string) (*rpcClient, error) {
	if rpcClientApi != nil {
		return rpcClientApi, nil
	}

	rpcClientApiRW.Lock()
	defer rpcClientApiRW.Unlock()

	if rpcClientApi != nil {
		return rpcClientApi, nil
	}

	token, err := fa.RetryAuthNew(ctx, []auth.Permission{"admin"}) // using the miner token for worker rpc server.
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

	rpcClientApi = &rpcClient{wapi, closer}
	return rpcClientApi, nil
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

	log.Infof("Selected Commit2 Service: %s", rCfg.SvcUri)

	// connect to remote worker
	rClient, err := ConnectHlmWorker(ctx, napi, rCfg.SvcUri)
	if err != nil {
		return nil, errors.As(err)
	}

	// do work
	return rClient.SealCommit2(ctx, api.SectorRef{SectorID: task.SectorID, ProofType: task.ProofType, TaskKey: task.Key()}, c1out)
}
