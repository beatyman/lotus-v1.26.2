package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
	"net/http"
	"strings"
	"sync"
)

var (
	rpcClientsRW = &sync.RWMutex{}
	rpcClients   = make(map[string]*rpcClient)
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

func GetWorkerClient(ctx context.Context, mApi *api.RetryHlmMinerSchedulerAPI, url string) (*rpcClient, error) {
	rpcClientsRW.RLock()
	cli, ok := rpcClients[url]
	rpcClientsRW.RUnlock()

	if ok && cli != nil {
		return cli, nil
	}

	rpcClientsRW.Lock()
	defer rpcClientsRW.Unlock()

	token, err := mApi.RetryAuthNew(ctx, []auth.Permission{"admin"}) // using the miner token for worker rpc server.
	if err != nil {
		return nil, errors.New("creating auth token for remote connection").As(err)
	}
	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))
	urlInfo := strings.Split(url, ":")
	if len(urlInfo) != 2 {
		return nil, errors.New("error url format")
	}
	rpcUrl := fmt.Sprintf("ws://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])
	wApi, closer, err := client.NewWorkerHlmRPC(ctx, rpcUrl, headers)
	if err != nil {
		return nil, errors.New("creating jsonrpc client").As(err)
	}
	cli = &rpcClient{wApi, closer}
	rpcClients[url] = cli

	return cli, nil
}

func CallCommit2Service(ctx context.Context, task ffiwrapper.WorkerTask, c1out storiface.Commit1Out) (storiface.Proof, error) {
	mApi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}

	c2worker, err := mApi.RetrySelectCommit2Service(ctx, task.SectorID)
	if err != nil {
		return nil, errors.As(err)
	}
	if len(c2worker.Proof) > 0 {
		if data, err := hex.DecodeString(c2worker.Proof); err == nil {
			log.Infof("Task(%v) Cached Commit2 Service: %s", task.Key(), c2worker.WorkerId)
			return data, nil
		}
	}

	log.Infof("Task(%v) Selected Commit2 Service: %s", task.Key(), c2worker.Url)
	cli, err := GetWorkerClient(ctx, mApi, c2worker.Url)
	if err != nil {
		return nil, errors.As(err)
	}
	return cli.SealCommit2(ctx, api.SectorRef{SectorID: task.SectorID, ProofType: task.ProofType, TaskKey: task.Key()}, c1out)
}

func CallProveReplicaUpdate2Service(ctx context.Context, sector storiface.SectorRef, task ffiwrapper.WorkerTask, vanillaProofs storiface.ReplicaVanillaProofs) (storage.ReplicaUpdateProof, error) {
	mApi, err := GetNodeApi()
	if err != nil {
		return nil, errors.As(err)
	}
	c2worker, err := mApi.RetrySelectCommit2Service(ctx, task.SectorID)
	if err != nil {
		return nil, errors.As(err)
	}
	if len(c2worker.Proof) > 0 {
		if data, err := hex.DecodeString(c2worker.Proof); err == nil {
			log.Infof("Task(%v) Cached ProveReplicaUpdate2 Service: %s", task.Key(), c2worker.WorkerId)
			return data, nil
		}
	}
	log.Infof("Task(%v) Selected ProveReplicaUpdate2 Service: %s", task.Key(), c2worker.Url)
	cli, err := GetWorkerClient(ctx, mApi, c2worker.Url)
	if err != nil {
		return nil, errors.As(err)
	}
	return cli.ProveReplicaUpdate2(ctx, api.SectorRef{SectorID: task.SectorID, ProofType: task.ProofType, TaskKey: task.Key(), SectorKey: task.SectorKey, NewUnsealed: task.NewUnsealed, NewSealed: task.NewSealed}, vanillaProofs)
}
