package etcd

import (
	"context"
	"fmt"

	"github.com/gwaylib/errors"
	"go.etcd.io/etcd/clientv3/concurrency"
)

type Mutex interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	Close() error
}

type mutex struct {
	sess   *concurrency.Session
	locker *concurrency.Mutex
}

func (m *mutex) Lock(ctx context.Context) error {
	return m.locker.Lock(ctx)
}

func (m *mutex) Unlock(ctx context.Context) error {
	return m.locker.Unlock(ctx)
}

func (m *mutex) Close() error {
	return m.sess.Close()
}

func GetMutex(key string) (Mutex, error) {
	cli, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	s1, err := concurrency.NewSession(cli)
	if err != nil {
		return nil, errors.As(err)
	}
	return &mutex{
		sess:   s1,
		locker: concurrency.NewMutex(s1, fmt.Sprintf("/%s/", key)),
	}, nil
}
