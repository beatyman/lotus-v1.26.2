package auth

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

type CryptoData struct {
	Old    bool
	Passwd string // inupt passwd
	Data   []byte // decoded data
}

var (
	memCryptoLk    = sync.Mutex{}
	memCryptoCache = map[string]*CryptoData{}

	inputLastPasswd    = ""
	inputCryptoPwdLk   = sync.Mutex{}
	inputCryptoPwdName = ""
	inputCryptoPwdDir  = filepath.Join(os.TempDir(), ".wallet-encode")
	inputCryptoPwdFile = ""
	inputCryptoPwdRet  = make(chan error, 1)
)

func RegisterCryptoCache(key string, value *CryptoData) {
	memCryptoLk.Lock()
	defer memCryptoLk.Unlock()
	memCryptoCache[key] = value
}
func DeleteCryptoCache(key string) {
	memCryptoLk.Lock()
	defer memCryptoLk.Unlock()
	delete(memCryptoCache, key)
}

func InputCryptoStatus() string {
	inputCryptoPwdLk.Lock()
	defer inputCryptoPwdLk.Unlock()
	return inputCryptoPwdName
}
func InputCryptoPwd(ctx context.Context, pwd string) error {
	inputCryptoPwdLk.Lock()
	defer inputCryptoPwdLk.Unlock()
	if len(inputCryptoPwdName) == 0 {
		return nil
	}
	if err := WriteCryptoUnixPwd(ctx, inputCryptoPwdFile, pwd); err != nil {
		return errors.As(err)
	}
	err := <-inputCryptoPwdRet
	if err != nil {
		return errors.As(err)
	}
	return nil
}
func InputCryptoUnixStatus(ctx context.Context) (string, string, error) {
	inputCryptoPwdLk.Lock()
	defer inputCryptoPwdLk.Unlock()
	files, err := ioutil.ReadDir(inputCryptoPwdDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", "", errors.As(err)
		}
		return "", "", errors.ErrNoData
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}

		sFile := filepath.Join(inputCryptoPwdDir, f.Name())
		name, err := getCryptoUnixStatus(ctx, sFile)
		if err != nil {
			// restart the lotus-daemon when waiting input could happen this.
			log.Info("Remove file %s by %s", sFile, errors.As(err).Code())
			os.Remove(sFile)
			continue
		}
		return name, sFile, nil
	}
	return "", "", errors.ErrNoData
}
func getCryptoUnixStatus(ctx context.Context, unixSFile string) (string, error) {
	raddr := net.UnixAddr{Name: unixSFile, Net: "unix"}
	var d net.Dialer
	d.LocalAddr = nil // if you have a local addr, add it here
	conn, err := d.DialContext(ctx, "unix", raddr.String())
	if err != nil {
		return "", errors.As(err)
	}
	defer conn.Close()
	p := map[string]string{
		"Method": "Status",
	}
	input, err := json.Marshal(&p)
	if err != nil {
		return "", errors.As(err)
	}
	if err := WriteSocketTextReq(conn, input); err != nil {
		return "", errors.As(err)
	}
	resp, err := ReadSocketTextResp(conn)
	if err != nil {
		return "", errors.As(err)
	}
	switch resp["Code"].(string) {
	case "200":
		data := resp["Data"].(map[string]interface{})
		return data["Name"].(string), nil
	default:
		return "", errors.Parse(resp["Err"].(string))
	}

}

func WriteCryptoUnixPwd(ctx context.Context, unixSFile, passwd string) error {
	raddr := net.UnixAddr{Name: unixSFile, Net: "unix"}
	var d net.Dialer
	d.LocalAddr = nil // if you have a local addr, add it here
	conn, err := d.DialContext(ctx, "unix", raddr.String())
	if err != nil {
		return errors.As(err)
	}
	defer conn.Close()
	p := map[string]string{
		"Method": "Passwd",
		"Passwd": passwd,
	}
	input, err := json.Marshal(&p)
	if err != nil {
		return errors.As(err)
	}
	if err := WriteSocketTextReq(conn, input); err != nil {
		return errors.As(err)
	}
	return nil
}

func daemonCryptoPwd(ctx context.Context, ln net.Listener) (string, error) {
	result := make(chan interface{}, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				result <- errors.As(err)
				return
			}
			defer conn.Close()

			req, err := ReadSocketTextReq(conn)
			if err != nil {
				result <- errors.As(err)
				return
			}
			switch req["Method"] {
			case "Passwd":
				result <- req["Passwd"]
				return
			default:
				// output the input name and continue waitting input.
				if err := WriteSocketSucResp(conn, 200, map[string]interface{}{
					"Name": inputCryptoPwdName,
				}); err != nil {
					result <- errors.As(err)
					return
				}

				//	continue
			}
		}
	}()
	select {
	case r := <-result:
		err, ok := r.(errors.Error)
		if ok {
			return "", err
		}
		return r.(string), nil
	case <-ctx.Done():
		return "", errors.New("ctx canceled")
	}
}

// thread 1
// go func(){
//	for {
//		name := StatusInputCryptoPwd()
//		if len(name) > 0 {
//			InputCryptoPWd(...)
//		}
//		// TODO: set the next try by yourself.
//	}
// }()
//
// thread 2
// ret, err := DecodeData(...)
// ...
func DecodeData(ctx context.Context, key string, eData []byte) (*CryptoData, error) {
	memCryptoLk.Lock()
	defer memCryptoLk.Unlock()

	value, ok := memCryptoCache[key]
	if ok {
		return value, nil
	}

	// try the last passwd
	data, err := MixDecript(eData, inputLastPasswd)
	if err == nil {
		wData := &CryptoData{Old: false, Passwd: inputLastPasswd, Data: data}
		memCryptoCache[key] = wData
		return wData, nil
	}

	if err := os.MkdirAll(inputCryptoPwdDir, 0700); err != nil {
		return nil, errors.As(err)
	}
	sFile := filepath.Join(inputCryptoPwdDir, uuid.New().String())
	defer os.Remove(sFile)

	raddr := net.UnixAddr{Name: sFile, Net: "unix"}
	// unix listen
	ln, err := net.ListenUnix("unix", &raddr)
	if err != nil {
		return nil, errors.As(err)
	}
	defer ln.Close()

	// need input passwd
	inputCryptoPwdLk.Lock()
	inputCryptoPwdName = key
	inputCryptoPwdFile = sFile
	inputCryptoPwdLk.Unlock()
	defer func() {
		inputCryptoPwdLk.Lock()
		inputCryptoPwdName = ""
		inputCryptoPwdFile = ""
		inputCryptoPwdLk.Unlock()
	}()
	try := 0
	for {
		log.Warnf("Waitting input password for : %s, try: %d", key, try)
		passwd, err := daemonCryptoPwd(ctx, ln)
		if err != nil {
			return nil, errors.As(err)
		}
		try++

		old := false
		data, err := MixDecript(eData, passwd)
		if err != nil {
			// try the old, if it's success, do a upgrade.
			data, err = OldMixDecript(eData, passwd)
			if err != nil {
				inputCryptoPwdRet <- errors.As(err)
				continue
			}
			// pass
			old = true
		}
		log.Infof("Decode %s success, old cert:%t.", key, old)
		// decode success
		// response the caller that decode has success.
		inputCryptoPwdRet <- nil

		inputLastPasswd = passwd
		wData := &CryptoData{Old: old, Passwd: passwd, Data: data}
		memCryptoCache[key] = wData
		return wData, nil
	}
}

func EncodeData(key string, value []byte, passwd string) ([]byte, error) {
	eData, err := MixEncript(value, passwd)
	if err != nil {
		return nil, errors.As(err)
	}

	memCryptoLk.Lock()
	defer memCryptoLk.Unlock()

	memCryptoCache[key] = &CryptoData{Passwd: passwd, Data: value}
	return eData, nil
}
