package main

import (
	"context"
	"net/http"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/specs-actors/actors/abi"
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

func ConnectHlmWorker(ctx context.Context, fa api.Common, url string) (*rpcClient, error) {
	token, err := fa.AuthNew(ctx, []auth.Permission{"admin"})
	if err != nil {
		return nil, errors.New("creating auth token for remote connection").As(err)
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))

	wapi, closer, err := client.NewWorkerHlmRPC(url, headers)
	if err != nil {
		return nil, errors.New("creating jsonrpc client").As(err)
	}

	return &rpcClient{wapi, closer}, nil
}

func CallCommit2Service(ctx context.Context, sector abi.SectorID, commit1Out storage.Commit1Out) (storage.Proof, error) {
	napi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}
	rCfg, err := napi.SelectCommit2Service(ctx, sector)
	if err != nil {
		return nil, errors.As(err)
	}

	// connect to remote worker
	rClient, err := ConnectHlmWorker(ctx, napi, rCfg.SvcUri)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rClient.Close()

	// do work
	return rClient.SealCommit2(ctx, sector, commit1Out)
}
