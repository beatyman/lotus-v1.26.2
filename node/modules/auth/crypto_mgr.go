package auth

import (
	"sync"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

type CryptoData struct {
	Passwd string // inupt passwd
	Data   []byte // decoded data
}

var (
	memCryptoLk    = sync.Mutex{}
	memCryptoCache = map[string]*CryptoData{}

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
	inputCryptoPwdName = ""
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
func DecodeData(key string, eData []byte) (*CryptoData, error) {
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

	// TODO: dead lock?
	try := 0
	for {
		log.Warnf("Waitting input password for : %s, try: %d", key, try)
		passwd := <-inputCryptoPwdCh
		try++
		data, err := MixDecript(eData, passwd)
		if err != nil {
			inputCryptoPwdRet <- errors.As(err)
			continue
		}
		log.Infof("Decode %s success.", key)
		// decode success
		// response the caller that decode has success.
		inputCryptoPwdRet <- nil

		wData := &CryptoData{Passwd: passwd, Data: data}
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
