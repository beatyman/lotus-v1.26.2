package etcd

import (
	"context"
	"sync"

	"github.com/gwaylib/errors"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	etcMutex   = sync.Mutex{}
	etcdAddr   = "" //http://127.0.0.1:2379
	etcdClient *clientv3.Client
)

func InitAddr(addr string) {
	etcMutex.Lock()
	defer etcMutex.Unlock()
	etcdAddr = addr
}

func HasOn() bool {
	etcMutex.Lock()
	defer etcMutex.Unlock()
	return len(etcdAddr) > 0
}

func Client() (*clientv3.Client, error) {
	etcMutex.Lock()
	defer etcMutex.Unlock()
	if len(etcdAddr) == 0 {
		return nil, errors.ErrNoData
	}

	if etcdClient == nil {
		client, err := clientv3.NewFromURL(etcdAddr)
		if err != nil {
			etcdClient = nil
			return nil, errors.As(err)
		}
		etcdClient = client
	}

	return etcdClient, nil
}

func closeClient() {
	etcMutex.Lock()
	defer etcMutex.Unlock()
	if etcdClient == nil {
		return
	}
	if err := etcdClient.Close(); err != nil {
		log.Warn(errors.As(err))
	}
	etcdClient = nil
	return
}

// Put puts a key-value pair into etcd.
// Note that key,value can be plain bytes array and string is
// an immutable representation of that bytes array.
// To get a string of bytes, do string([]byte{0x10, 0x20}).
func Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	client, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	resp, err := client.Put(ctx, key, val, opts...)
	if err != nil {
		closeClient()
	}
	return resp, err
}

// Get retrieves keys.
// By default, Get will return the value for "key", if any.
// When passed WithRange(end), Get will return the keys in the range [key, end).
// When passed WithFromKey(), Get returns keys greater than or equal to key.
// When passed WithRev(rev) with rev > 0, Get retrieves keys at the given revision;
// if the required revision is compacted, the request will fail with ErrCompacted .
// When passed WithLimit(limit), the number of returned keys is bounded by limit.
// When passed WithSort(), the keys will be sorted.
func Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	client, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	resp, err := client.Get(ctx, key, opts...)
	if err != nil {
		closeClient()
	}
	return resp, err
}

// Delete deletes a key, or optionally using WithRange(end), [key, end).
func Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	client, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	resp, err := client.Delete(ctx, key, opts...)
	if err != nil {
		closeClient()
	}
	return resp, err
}

// Compact compacts etcd KV history before the given rev.
func Compact(ctx context.Context, rev int64, opts ...clientv3.CompactOption) (*clientv3.CompactResponse, error) {
	client, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	resp, err := client.Compact(ctx, rev, opts...)
	if err != nil {
		closeClient()
	}
	return resp, err
}

// Do applies a single Op on KV without a transaction.
// Do is useful when creating arbitrary operations to be issued at a
// later time; the user can range over the operations, calling Do to
// execute them. Get/Put/Delete, on the other hand, are best suited
// for when the operation should be issued at the time of declaration.
func Do(ctx context.Context, op clientv3.Op) (clientv3.OpResponse, error) {
	client, err := Client()
	if err != nil {
		return clientv3.OpResponse{}, errors.As(err)
	}
	resp, err := client.Do(ctx, op)
	if err != nil {
		closeClient()
	}
	return resp, err
}

// Txn creates a transaction.
func NewTxn(ctx context.Context) (*Txn, error) {
	client, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	return &Txn{tx: client.Txn(ctx)}, nil
}

func CommitTxn(txn *Txn) (*clientv3.TxnResponse, error) {
	resp, err := txn.tx.Commit()
	if err != nil {
		closeClient()
	}
	return resp, err
}
