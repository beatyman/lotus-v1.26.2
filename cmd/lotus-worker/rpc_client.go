package main

import (
	"context"
	"net/http"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
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
