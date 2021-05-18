package auth

import (
	"context"
	"sync"

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
	inputCryptoPwdCh   = make(chan string, 1)
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
func InputCryptoPwd(pwd string) error {
	inputCryptoPwdLk.Lock()
	defer inputCryptoPwdLk.Unlock()
	if len(inputCryptoPwdName) == 0 {
		return nil
	}
	inputCryptoPwdCh <- pwd
	err := <-inputCryptoPwdRet
	if err != nil {
		return errors.As(err)
	}
	return nil
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

	// need input passwd
	inputCryptoPwdLk.Lock()
	inputCryptoPwdName = key
	inputCryptoPwdLk.Unlock()
	defer func() {
		inputCryptoPwdLk.Lock()
		inputCryptoPwdName = ""
		inputCryptoPwdLk.Unlock()
	}()

	// try the last passwd
	data, err := MixDecript(eData, inputLastPasswd)
	if err == nil {
		wData := &CryptoData{Old: false, Passwd: inputLastPasswd, Data: data}
		memCryptoCache[key] = wData
		return wData, nil
	}

	// TODO: dead lock?
	try := 0
	for {
		log.Warnf("Waitting input password for : %s, try: %d", key, try)
		passwd := ""
		select {
		case pwd := <-inputCryptoPwdCh:
			passwd = pwd
		case <-ctx.Done():
			return nil, errors.New("input canceled")
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
