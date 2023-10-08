package apiaddr

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/filecoin-project/lotus/node/repo"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var log = logging.Logger("apiaddr")

var (
	infoWithToken = regexp.MustCompile("^[a-zA-Z0-9\\-_]+?\\.[a-zA-Z0-9\\-_]+?\\.([a-zA-Z0-9\\-_]+)?:.+$")
)

type APIInfo struct {
	Addr  string
	Token []byte
}

func ParseApiInfo(s string) APIInfo {
	var tok []byte
	if infoWithToken.Match([]byte(s)) {
		sp := strings.SplitN(s, ":", 2)
		tok = []byte(sp[0])
		s = sp[1]
	}

	return APIInfo{
		Addr:  s,
		Token: tok,
	}
}

func (a *APIInfo) String() string {
	return string(a.Token) + ":" + a.Addr
}

func (a *APIInfo) DialArgs(version string, repoType repo.RepoType) (string, error) {
	// TODO: force to wss or https when c2 fix done.

	ma, err := multiaddr.NewMultiaddr(a.Addr)
	if err == nil {
		_, addr, err := manet.DialArgs(ma)
		if err != nil {
			return "", err
		}

		switch repoType {
		case repo.FullNode, repo.StorageMiner, repo.Markets:
			return "ws://" + addr + "/rpc/" + version, nil
		default:
			return "ws://" + addr + "/rpc/" + version, nil
		}

	}

	_, err = url.Parse(a.Addr)
	if err != nil {
		return "", err
	}
	return a.Addr + "/rpc/" + version, nil
}
func (a *APIInfo) Host() (string, error) {
	ma, err := multiaddr.NewMultiaddr(a.Addr)
	if err == nil {
		_, addr, err := manet.DialArgs(ma)
		if err != nil {
			return "", err
		}

		return addr, nil
	}

	spec, err := url.Parse(a.Addr)
	if err != nil {
		return "", err
	}
	return spec.Host, nil
}

func (a *APIInfo) AuthHeader() http.Header {
	if len(a.Token) != 0 {
		headers := http.Header{}
		headers.Add("Authorization", "Bearer "+string(a.Token))
		return headers
	}
	log.Warn("API Token not set and requested, capabilities might be limited.")
	return nil
}
func ParseApiInfoMulti(s string) []APIInfo {
	var apiInfos []APIInfo

	allAddrs := strings.SplitN(s, ",", -1)

	for _, addr := range allAddrs {
		apiInfos = append(apiInfos, ParseApiInfo(addr))
	}

	return apiInfos
}
