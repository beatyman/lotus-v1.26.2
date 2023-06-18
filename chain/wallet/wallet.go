package wallet

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/wallet/encode"
	"github.com/gwaylib/errors"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	_ "github.com/filecoin-project/lotus/lib/sigs/bls" // enable bls signatures
	_ "github.com/filecoin-project/lotus/lib/sigs/delegated"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp" // enable secp signatures
)

var log = logging.Logger("wallet")

const (
	KNamePrefix  = "wallet-"
	KTrashPrefix = "trash-"
	KDefault     = "default"
)

type LocalWallet struct {
	keys     map[address.Address]*key.Key
	keystore types.KeyStore

	lk sync.Mutex
}

type Default interface {
	GetDefault(ctx context.Context) (address.Address, error)
	SetDefault(a address.Address) error
}

func NewWallet(keystore types.KeyStore) (*LocalWallet, error) {
	w := &LocalWallet{
		keys:     make(map[address.Address]*key.Key),
		keystore: keystore,
	}

	return w, nil
}

func KeyWallet(keys ...*key.Key) *LocalWallet {
	m := make(map[address.Address]*key.Key)
	for _, key := range keys {
		m[key.Address] = key
	}

	return &LocalWallet{
		keys: m,
	}
}

func (w *LocalWallet) WalletEncode(ctx context.Context, addr address.Address, passwd string) error {
	k, err := w.findKey(ctx, addr)
	if err != nil {
		return err
	}
	if k == nil {
		return xerrors.Errorf("signing using key '%s': %w", addr.String(), types.ErrKeyInfoNotFound)
	}
	if len(passwd) == 0 {
		return xerrors.Errorf("password not set")
	}
	if k.Encrypted {
		return xerrors.Errorf("the address is already encrypted.")
	}

	dsName := KNamePrefix + addr.String()
	eData, err := encode.EncodeData(dsName, k.PrivateKey, passwd)
	if err != nil {
		return errors.As(err)
	}
	k.PrivateKey = eData
	k.DsName = dsName
	k.Encrypted = true

	if err := w.keystore.Delete(dsName); err != nil {
		return xerrors.Errorf("delete to keystore: %w", err)
	}
	if err := w.keystore.Put(dsName, k.KeyInfo); err != nil {
		return xerrors.Errorf("saving to keystore: %w", err)
	}
	w.keys[addr] = k
	return nil
}

func (w *LocalWallet) WalletSign(ctx context.Context, addr address.Address, msg []byte, meta api.MsgMeta) (*crypto.Signature, error) {
	ki, err := w.findKey(ctx, addr)
	if err != nil {
		return nil, err
	}
	if ki == nil {
		return nil, xerrors.Errorf("signing using key '%s': %w", addr.String(), types.ErrKeyInfoNotFound)
	}
	// by zhoushuyue
	privateKey := ki.PrivateKey
	if ki.Encrypted {
		dsName := KNamePrefix + addr.String()
		cData, err := encode.DecodeData(ctx, dsName, ki.PrivateKey)
		if err != nil {
			return nil, errors.As(err)
		}
		privateKey = cData.Data
	}
	// end by zhoushuyue

	return sigs.Sign(key.ActSigType(ki.Type), privateKey, msg)
}

func (w *LocalWallet) findKey(ctx context.Context, addr address.Address) (*key.Key, error) {
	w.lk.Lock()
	defer w.lk.Unlock()

	k, ok := w.keys[addr]
	if ok {
		return k, nil
	}
	if w.keystore == nil {
		log.Warn("findKey didn't find the key in in-memory wallet")
		return nil, nil
	}

	ki, err := w.tryFind(addr)
	if err != nil {
		if xerrors.Is(err, types.ErrKeyInfoNotFound) {
			return nil, nil
		}
		return nil, xerrors.Errorf("getting from keystore: %w", err)
	}
	var cData *encode.CryptoData
	// Try upgrade the root cert
	if ki.Encrypted {
		cData, err = encode.DecodeData(ctx, ki.DsName, ki.PrivateKey)
		if err != nil {
			return nil, errors.As(err)
		}
	}
	k, err = key.NewKey(ctx, ki)
	if err != nil {
		return nil, xerrors.Errorf("decoding from keystore: %w", err)
	}

	// upgrade the old cert to the new cert.
	if cData != nil && cData.Old {
		dsName := ki.DsName
		eData, err := encode.EncodeData(dsName, cData.Data, cData.Passwd)
		if err != nil {
			return nil, errors.As(err)
		}
		k.PrivateKey = eData
		k.DsName = dsName
		k.Encrypted = true

		if err := w.keystore.Delete(dsName); err != nil {
			return nil, xerrors.Errorf("delete to keystore: %w", err)
		}
		if err := w.keystore.Put(dsName, k.KeyInfo); err != nil {
			return nil, xerrors.Errorf("saving to keystore: %w", err)
		}
		cData.Old = false
		encode.RegisterCryptoCache(dsName, cData)
		log.Infof("Upgrade %s cert to %s", dsName, encode.RootKeyHash())
	}
	w.keys[k.Address] = k
	return k, nil
}

func (w *LocalWallet) tryFind(addr address.Address) (types.KeyInfo, error) {
	ki, err := w.keystore.Get(KNamePrefix + addr.String())
	if err == nil {
		return ki, err
	}

	if !xerrors.Is(err, types.ErrKeyInfoNotFound) {
		return types.KeyInfo{}, err
	}

	// We got an ErrKeyInfoNotFound error
	// Try again, this time with the testnet prefix

	tAddress, err := swapMainnetForTestnetPrefix(addr.String())
	if err != nil {
		return types.KeyInfo{}, err
	}

	ki, err = w.keystore.Get(KNamePrefix + tAddress)
	if err != nil {
		return types.KeyInfo{}, err
	}

	// We found it with the testnet prefix
	// Add this KeyInfo with the mainnet prefix address string
	err = w.keystore.Put(KNamePrefix+addr.String(), ki)
	if err != nil {
		return types.KeyInfo{}, err
	}

	return ki, nil
}

func (w *LocalWallet) WalletExport(ctx context.Context, addr address.Address) (*types.KeyInfo, error) {
	k, err := w.findKey(ctx, addr)
	if err != nil {
		return nil, xerrors.Errorf("failed to find key to export: %w", err)
	}
	if k == nil {
		return nil, xerrors.Errorf("key not found for %s", addr)
	}

	return &k.KeyInfo, nil
}

func (w *LocalWallet) WalletImport(ctx context.Context, ki *types.KeyInfo) (address.Address, error) {
	w.lk.Lock()
	defer w.lk.Unlock()

	k, err := key.NewKey(ctx, *ki)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to make key: %w", err)
	}

	if err := w.keystore.Put(KNamePrefix+k.Address.String(), k.KeyInfo); err != nil {
		return address.Undef, xerrors.Errorf("saving to keystore: %w", err)
	}

	return k.Address, nil
}

func (w *LocalWallet) WalletList(ctx context.Context) ([]address.Address, error) {
	all, err := w.keystore.List()
	if err != nil {
		return nil, xerrors.Errorf("listing keystore: %w", err)
	}

	sort.Strings(all)

	seen := map[address.Address]struct{}{}
	out := make([]address.Address, 0, len(all))
	for _, a := range all {
		if strings.HasPrefix(a, KNamePrefix) {
			name := strings.TrimPrefix(a, KNamePrefix)
			addr, err := address.NewFromString(name)
			if err != nil {
				return nil, xerrors.Errorf("converting name to address: %w", err)
			}
			if _, ok := seen[addr]; ok {
				continue // got duplicate with a different prefix
			}
			seen[addr] = struct{}{}
			out = append(out, addr)

			// try decode. by zsy
			if _, err := w.findKey(ctx, addr); err != nil {
				return nil, errors.As(err)
			}
			// end by zsy

		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].String() < out[j].String()
	})

	return out, nil
}

func (w *LocalWallet) GetDefault(ctx context.Context) (address.Address, error) {
	w.lk.Lock()
	defer w.lk.Unlock()

	ki, err := w.keystore.Get(KDefault)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to get default key: %w", err)
	}

	k, err := key.NewKey(ctx, ki)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to read default key from keystore: %w", err)
	}

	return k.Address, nil
}

func (w *LocalWallet) SetDefault(a address.Address) error {
	w.lk.Lock()
	defer w.lk.Unlock()

	ki, err := w.keystore.Get(KNamePrefix + a.String())
	if err != nil {
		return err
	}

	if err := w.keystore.Delete(KDefault); err != nil {
		if !xerrors.Is(err, types.ErrKeyInfoNotFound) {
			log.Warnf("failed to unregister current default key: %s", err)
		}
	}

	if err := w.keystore.Put(KDefault, ki); err != nil {
		return err
	}

	return nil
}

func (w *LocalWallet) WalletNew(ctx context.Context, typ types.KeyType, passwd string) (address.Address, error) {
	w.lk.Lock()
	defer w.lk.Unlock()

	k, err := key.GenerateKey(typ)
	if err != nil {
		return address.Undef, err
	}

	// by zhoushuyue
	dsName := KNamePrefix + k.Address.String()
	// encode the private key
	if len(passwd) > 0 {
		eData, err := encode.EncodeData(dsName, k.KeyInfo.PrivateKey, passwd)
		if err != nil {
			return address.Undef, errors.As(err)
		}
		k.KeyInfo.PrivateKey = eData
		k.KeyInfo.DsName = dsName
		k.KeyInfo.Encrypted = true
	}
	// end by zhoushuyue

	if err := w.keystore.Put(dsName, k.KeyInfo); err != nil {
		return address.Undef, xerrors.Errorf("saving to keystore: %w", err)
	}
	w.keys[k.Address] = k

	_, err = w.keystore.Get(KDefault)
	if err != nil {
		if !xerrors.Is(err, types.ErrKeyInfoNotFound) {
			return address.Undef, err
		}

		if err := w.keystore.Put(KDefault, k.KeyInfo); err != nil {
			return address.Undef, xerrors.Errorf("failed to set new key as default: %w", err)
		}
	}

	return k.Address, nil
}

func (w *LocalWallet) WalletHas(ctx context.Context, addr address.Address) (bool, error) {
	k, err := w.findKey(ctx, addr)
	if err != nil {
		return false, err
	}
	return k != nil, nil
}

func (w *LocalWallet) walletDelete(ctx context.Context, addr address.Address) error {
	k, err := w.findKey(ctx, addr)

	if err != nil {
		return xerrors.Errorf("failed to delete key %s : %w", addr, err)
	}
	if k == nil {
		return nil // already not there
	}

	w.lk.Lock()
	defer w.lk.Unlock()

	// no trash because has backup when create the key. by zhoushuyue
	//if err := w.keystore.Delete(KTrashPrefix + k.Address.String()); err != nil && !xerrors.Is(err, types.ErrKeyInfoNotFound) {
	//	return xerrors.Errorf("failed to purge trashed key %s: %w", addr, err)
	//}
	//
	//if err := w.keystore.Put(KTrashPrefix+k.Address.String(), k.KeyInfo); err != nil {
	//	return xerrors.Errorf("failed to mark key %s as trashed: %w", addr, err)
	//}

	if err := w.keystore.Delete(KNamePrefix + k.Address.String()); err != nil {
		return xerrors.Errorf("failed to delete key %s: %w", addr, err)
	}

	tAddr, err := swapMainnetForTestnetPrefix(addr.String())
	if err != nil {
		return xerrors.Errorf("failed to swap prefixes: %w", err)
	}

	// TODO: Does this always error in the not-found case? Just ignoring an error return for now.
	_ = w.keystore.Delete(KNamePrefix + tAddr)

	delete(w.keys, addr)

	// by zhoushuyue
	// delete the decripted cache
	encode.DeleteCryptoCache(KNamePrefix + k.Address.String())
	// end by zhoushuyue

	return nil
}

func (w *LocalWallet) deleteDefault() {
	w.lk.Lock()
	defer w.lk.Unlock()
	if err := w.keystore.Delete(KDefault); err != nil {
		if !xerrors.Is(err, types.ErrKeyInfoNotFound) {
			log.Warnf("failed to unregister current default key: %s", err)
		}
	}
}

func (w *LocalWallet) WalletDelete(ctx context.Context, addr address.Address) error {
	if err := w.walletDelete(ctx, addr); err != nil {
		return xerrors.Errorf("wallet delete: %w", err)
	}

	if def, err := w.GetDefault(ctx); err == nil {
		if def == addr {
			w.deleteDefault()
		}
	}
	return nil
}

func (w *LocalWallet) Get() api.Wallet {
	if w == nil {
		return nil
	}

	return w
}

var _ api.Wallet = &LocalWallet{}

func swapMainnetForTestnetPrefix(addr string) (string, error) {
	aChars := []rune(addr)
	prefixRunes := []rune(address.TestnetPrefix)
	if len(prefixRunes) != 1 {
		return "", xerrors.Errorf("unexpected prefix length: %d", len(prefixRunes))
	}

	aChars[0] = prefixRunes[0]
	return string(aChars), nil
}

type nilDefault struct{}

func (n nilDefault) GetDefault(ctx context.Context) (address.Address, error) {
	return address.Undef, nil
}

func (n nilDefault) SetDefault(a address.Address) error {
	return xerrors.Errorf("not supported; local wallet disabled")
}

var NilDefault nilDefault
var _ Default = NilDefault
