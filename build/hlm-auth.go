// Auth for wallet on spec node.
// the security is depended on the login of the node.
package build

import (
	"bytes"
	"io/ioutil"
	"os"
	"sync"

	"github.com/gwaylib/errors"
)

const (
	authDir  = "/etc/lotus"
	authFile = "/etc/lotus/auth.dat"
)

var (
	auth    = []byte{}
	authMut = sync.Mutex{}
)

func init() {
	LoadHlmAuth()
}

func LoadHlmAuth() error {
	authMut.Lock()
	defer authMut.Unlock()
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

	auth = data
	return nil
}

func IsHlmAuth(in []byte) bool {
	authMut.Lock()
	defer authMut.Unlock()

	// just a simple set
	return bytes.Compare(in, auth) == 0
}

func GetHlmAuth() []byte {
	authMut.Lock()
	defer authMut.Unlock()
	return auth
}
