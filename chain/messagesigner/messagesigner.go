package messagesigner

import (
	"bytes"
	"context"
	"sync"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	logging "github.com/ipfs/go-log/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/etcd"
)

const dsKeyActorNonce = "ActorNextNonce"

var log = logging.Logger("messagesigner")

type MpoolNonceAPI interface {
	GetNonce(address.Address) (uint64, error)
}

// MessageSigner keeps track of nonces per address, and increments the nonce
// when signing a message
type MessageSigner struct {
	wallet api.WalletAPI
	lk     sync.Mutex
	mpool  MpoolNonceAPI
	ds     datastore.Batching
}

func NewMessageSigner(wallet api.WalletAPI, mpool MpoolNonceAPI, ds dtypes.MetadataDS) *MessageSigner {
	ds = namespace.Wrap(ds, datastore.NewKey("/message-signer/"))
	return &MessageSigner{
		wallet: wallet,
		mpool:  mpool,
		ds:     ds,
	}
}

// SignMessage increments the nonce for the message From address, and signs
// the message
func (ms *MessageSigner) SignMessage(ctx context.Context, auth []byte, msg *types.Message, cb func(*types.SignedMessage) error) (*types.SignedMessage, error) {
	ms.lk.Lock()
	defer ms.lk.Unlock()

	// etcd lock
	etcdMutex, err := etcd.NewMutex(msg.From.String())
	if err != nil {
		return nil, xerrors.Errorf("failed to lock etcd: %w", err)
	}
	defer etcdMutex.Close()

	if err := etcdMutex.Lock(ctx); err != nil {
		return nil, xerrors.Errorf("failed to lock etcd: %w", err)
	}
	defer etcdMutex.Unlock(ctx)

	// Get the next message nonce
	nonce, err := ms.nextNonce(msg.From)
	if err != nil {
		return nil, xerrors.Errorf("failed to create nonce: %w", err)
	}

	// Sign the message with the nonce
	msg.Nonce = nonce

	mb, err := msg.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing message: %w", err)
	}

	sig, err := ms.wallet.WalletSign(ctx, auth, msg.From, mb.Cid().Bytes(), api.MsgMeta{
		Type:  api.MTChainMsg,
		Extra: mb.RawData(),
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to sign message: %w", err)
	}

	// Callback with the signed message
	smsg := &types.SignedMessage{
		Message:   *msg,
		Signature: *sig,
	}
	err = cb(smsg)
	if err != nil {
		return nil, err
	}

	// If the callback executed successfully, write the nonce to the datastore
	if err := ms.saveNonce(msg.From, nonce); err != nil {
		return nil, xerrors.Errorf("failed to save nonce: %w", err)
	}

	return smsg, nil
}

// nextNonce gets the next nonce for the given address.
// If there is no nonce in the datastore, gets the nonce from the message pool.
func (ms *MessageSigner) nextNonce(addr address.Address) (uint64, error) {
	// Nonces used to be created by the mempool and we need to support nodes
	// that have mempool nonces, so first check the mempool for a nonce for
	// this address. Note that the mempool returns the actor state's nonce
	// by default.
	nonce, err := ms.mpool.GetNonce(addr)
	if err != nil {
		return 0, xerrors.Errorf("failed to get nonce from mempool: %w", err)
	}

	// Get the next nonce for this address from the datastore
	addrNonceKey := ms.dstoreKey(addr)
	dsNonceBytes := []byte{}

	resp, err := etcd.Get(context.TODO(), addrNonceKey.String())
	if err != nil {
		// maybe is not found when the etcd has broken.
		log.Warn(xerrors.Errorf("failed to read nonce from etcd: %w", err))
	} else if len(resp.Kvs) > 0 {
		dsNonceBytes = resp.Kvs[0].Value
	}

	// no data from etcd, read local datastore
	if len(dsNonceBytes) == 0 {
		dsNonceBytes, err = ms.ds.Get(addrNonceKey)
	}

	switch {
	case xerrors.Is(err, datastore.ErrNotFound):
		// If a nonce for this address hasn't yet been created in the
		// datastore, just use the nonce from the mempool
		return nonce, nil

	case err != nil:
		return 0, xerrors.Errorf("failed to get nonce from datastore: %w", err)

	default:
		// There is a nonce in the datastore, so unmarshall it
		maj, dsNonce, err := cbg.CborReadHeader(bytes.NewReader(dsNonceBytes))
		if err != nil {
			return 0, xerrors.Errorf("failed to parse nonce from datastore: %w", err)
		}
		if maj != cbg.MajUnsignedInt {
			return 0, xerrors.Errorf("bad cbor type parsing nonce from datastore")
		}

		// The message pool nonce should be <= than the datastore nonce
		if nonce <= dsNonce {
			nonce = dsNonce
		} else {
			log.Warnf("mempool nonce was larger than datastore nonce (%d > %d)", nonce, dsNonce)
		}

		return nonce, nil
	}
}

// saveNonce increments the nonce for this address and writes it to the
// datastore
func (ms *MessageSigner) saveNonce(addr address.Address, nonce uint64) error {
	// Increment the nonce
	nonce++

	// Write the nonce to the datastore
	addrNonceKey := ms.dstoreKey(addr)
	buf := bytes.Buffer{}
	_, err := buf.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, nonce))
	if err != nil {
		return xerrors.Errorf("failed to marshall nonce: %w", err)
	}

	// put to etcd
	ctx := context.TODO()
	if _, err := etcd.Put(ctx, addrNonceKey.String(), string(buf.Bytes())); err != nil {
		return xerrors.Errorf("failed to write nonce to etcd: %w", err)
	}

	// put to local store
	err = ms.ds.Put(addrNonceKey, buf.Bytes())
	if err != nil {
		return xerrors.Errorf("failed to write nonce to datastore: %w", err)
	}
	return nil
}

func (ms *MessageSigner) dstoreKey(addr address.Address) datastore.Key {
	return datastore.KeyWithNamespaces([]string{dsKeyActorNonce, addr.String()})
}
