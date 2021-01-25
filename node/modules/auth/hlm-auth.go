// Auth for wallet on spec node.
// the security is depended on the login of the node.
package auth

import (
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
	lines := strings.Split(string(data), "\n")

	// format check
	tmpAuth := map[string]string{}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if len(trim) == 0 {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			continue
		}

		auth := strings.Split(trim, ":")
		if len(auth) < 1 {
			continue
		}
		key := strings.TrimSpace(auth[0])
		if len(key) == 0 {
			continue
		}
		_, ok := tmpAuth[key]
		if ok {
			return nil, errors.New("duplicate key").As(auth[0])
		}

		if len(auth) == 1 {
			// for the old version
			tmpAuth[key] = "*"
		} else {
			// for the scope version
			tmpAuth[key] = auth[1]
		}
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
		if !ok {
			continue
		}
		// server has configured any address is ok.
		if addrs == "*" {
			return true
		}

		if strings.Contains(addrs, authAddr) {
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
