package types

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/wallet/encode"
	"github.com/gwaylib/errors"
)

var (
	ErrKeyInfoNotFound = fmt.Errorf("key info not found")
	ErrKeyExists       = fmt.Errorf("key already exists")
)

// KeyType defines a type of a key
type KeyType string

func (kt *KeyType) UnmarshalJSON(bb []byte) error {
	{
		// first option, try unmarshaling as string
		var s string
		err := json.Unmarshal(bb, &s)
		if err == nil {
			*kt = KeyType(s)
			return nil
		}
	}

	{
		var b byte
		err := json.Unmarshal(bb, &b)
		if err != nil {
			return fmt.Errorf("could not unmarshal KeyType either as string nor integer: %w", err)
		}
		bst := crypto.SigType(b)

		switch bst {
		case crypto.SigTypeBLS:
			*kt = KTBLS
		case crypto.SigTypeSecp256k1:
			*kt = KTSecp256k1
		case crypto.SigTypeDelegated:
			*kt = KTDelegated
		default:
			return fmt.Errorf("unknown sigtype: %d", bst)
		}
		log.Warnf("deprecation: integer style 'KeyType' is deprecated, switch to string style")
		return nil
	}
}

const (
	KTBLS             KeyType = "bls"
	KTSecp256k1       KeyType = "secp256k1"
	KTSecp256k1Ledger KeyType = "secp256k1-ledger"
	KTDelegated       KeyType = "delegated"
)

// KeyInfo is used for storing keys in KeyStore
type KeyInfo struct {
	Type       KeyType
	PrivateKey []byte

	// by zhoushuyue
	DsName    string // when encrypted, it's need a not empty value.
	Encrypted bool
	// end by zhoushuyue
}

// need encode.InputCryptPwd if encrypted.
func (ki *KeyInfo) PlainPrivateKey(ctx context.Context) ([]byte, error) {
	if !ki.Encrypted {
		return ki.PrivateKey, nil
	}

	cData, err := encode.DecodeData(ctx, ki.DsName, ki.PrivateKey)
	if err != nil {
		return nil, errors.As(err)
	}
	return cData.Data, nil
}

// KeyStore is used for storing secret keys
type KeyStore interface {
	// List lists all the keys stored in the KeyStore
	List() ([]string, error)
	// Get gets a key out of keystore and returns KeyInfo corresponding to named key
	Get(string) (KeyInfo, error)
	// Put saves a key info under given name
	Put(string, KeyInfo) error
	// Delete removes a key from keystore
	Delete(string) error
}
