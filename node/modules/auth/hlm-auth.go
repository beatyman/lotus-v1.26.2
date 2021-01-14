// Auth for wallet on spec node.
// the security is depended on the login of the node.
package auth

import (
	"bytes"
	"encoding/csv"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/gwaylib/errors"
)

const (
	authDir  = "/etc/lotus"
	authFile = "/etc/lotus/auth.dat"
)

var (
	auths     = map[string]string{} // authkey:addrs
	authData  = []byte{}
	authMutex = sync.Mutex{}
)

func init() {
	LoadHlmAuth()
}

func parseHlmAuth(data []byte) (map[string]string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.Comma = ':'

	records, err := r.ReadAll()
	if err != nil {
		return nil, errors.As(err)
	}
	// format check
	tmpAuth := map[string]string{}
	for idx, line := range records {
		if len(line) != 2 {
			return nil, errors.New("error auth format").As(idx, line)
		}
		_, ok := tmpAuth[line[0]]
		if ok {
			return nil, errors.New("duplicate key").As(line[0])
		}
		tmpAuth[line[0]] = line[1]
	}
	return tmpAuth, nil
}

func LoadHlmAuth() error {
	data, err := ioutil.ReadFile(authFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err)
		}
		// auth not set
		return nil
	}

	tmpAuth, err := parseHlmAuth(data)
	if err != nil {
		return errors.As(err)
	}

	authMutex.Lock()
	auths = tmpAuth
	authData = data
	authMutex.Unlock()
	return nil
}

func IsHlmAuth(authAddr string, authData []byte) bool {
	// TODO: auth from etcd.
	authMutex.Lock()
	defer authMutex.Unlock()

	// no auth set
	if len(auths) == 0 {
		return true
	}

	tmpAuth, err := parseHlmAuth(authData)
	if err != nil {
		// authData can't parse
		return false
	}

	for key, _ := range tmpAuth {
		addrs, ok := auths[string(key)]
		if ok && strings.Contains(addrs, authAddr) {
			return true
		}
	}

	// not found the auth
	return false

}

func GetHlmAuth() []byte {
	authMutex.Lock()
	defer authMutex.Unlock()

	return authData
}

func IsHlmAuthOn() bool {
	authMutex.Lock()
	defer authMutex.Unlock()
	return len(auths) > 0
}
