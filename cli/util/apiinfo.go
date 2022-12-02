package cliutil

import (
	"github.com/filecoin-project/lotus/cli/util/apiaddr"
	logging "github.com/ipfs/go-log/v2"
)
var log = logging.Logger("cliutil")

type APIInfo = apiaddr.APIInfo

func ParseApiInfo(s string) APIInfo {
	return apiaddr.ParseApiInfo(s)
}