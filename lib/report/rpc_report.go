package report

import (
	"context"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/gwaylib/log"
	"sync"
)

type ReportRequest struct {
	DataType string `json:"data_type"`
	Data     []byte `json:"data"`
}

type ReportResponse struct {
	Code    int
	Message string
}

type Client struct {
	Report func(para *ReportRequest) *ReportResponse
}

var (
	once             sync.Once
	defaultRpcClient *Client
	defaultCloser    jsonrpc.ClientCloser
	ctx              = context.Background()
)

func init() {
	once.Do(func() {
		client := Client{}
		closer, err := jsonrpc.NewClient(ctx, "ws://localhost:1918/local/report", "ReportServer", &client, nil)
		if err != nil {
			log.Error(err)
			return
		}
		defaultCloser = closer
		defaultRpcClient = &client
	})
}

func SendRpcReport(data []byte) {
	if defaultRpcClient == nil {
		return
	}
	resp := defaultRpcClient.Report(&ReportRequest{
		DataType: "",
		Data:     data,
	})
	log.Info("Send Rpc Report Sectors .....")
	if resp.Code != 0 {
		log.Warn(resp.Message)
	}
}
func CloseRpcClient() {
	if defaultCloser != nil {
		defaultCloser()
	}
}
