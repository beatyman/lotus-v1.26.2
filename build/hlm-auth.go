// Auth for wallet on spec node.
// the security is depended on the login of the node.
package build

import (
	"bytes"
	"encoding/csv"
	"io/ioutil"
	"os"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/gwaylib/errors"
)

const (
	authDir  = "/etc/lotus"
	authFile = "/etc/lotus/auth.dat"
)

var (
	auth      = map[string][]byte{}
	authMutex = sync.Mutex{}
)

func init() {
	LoadHlmAuth()
}

func LoadHlmAuth() error {
	return loadHlmAuth()
}

func loadHlmAuth() error {
	data, err := ioutil.ReadFile(authFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.As(err)
		}
		// auth not set
		return nil
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.Comma = ':'

	records, err := r.ReadAll()
	if err != nil {
		return errors.As(err)
	}

	for idx, line := range records {
		if len(line) != 2 {
			return errors.New("error auth format").As(idx, line)
		}
		authMutex.Lock()
		auth[line[0]] = []byte(line[1])
		authMutex.Unlock()
	}

	return nil
}

func IsHlmAuth(key string, pwdIn []byte) bool {
	// TODO: auth from etcd.
	authMutex.Lock()
	defer authMutex.Unlock()
	if len(auth) == 0 {
		// no set for auth
		return true
	}
	pwd, ok := auth[key]
	if !ok {
		return false
	}

	return bytes.Compare(pwd, pwdIn) != 0
}

func GetHlmAuth(key address.Address) []byte {
	authMutex.Lock()
	defer authMutex.Unlock()

	pwd, ok := auth[key.String()]
	if !ok {
		return nil
	}
	return pwd
}
