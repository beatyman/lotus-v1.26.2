package etcd

import (
	"context"
	"fmt"
	"sync"

	"github.com/gwaylib/errors"
    "go.etcd.io/etcd/client/v3/concurrency"
)

type Mutex interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context)
	Close()
}

type mutex struct {
	memMutex sync.Mutex // make a memory mutex becase it can't lock in the same session.

	sess   *concurrency.Session
	locker *concurrency.Mutex
}

func (m *mutex) Lock(ctx context.Context) error {
	m.memMutex.Lock()
	if !HasOn() {
		return nil
	}

	if err := m.locker.Lock(ctx); err != nil {
		m.memMutex.Unlock()
		return err
	}

	// keep mem mutex
	return nil
}

func (m *mutex) Unlock(ctx context.Context) {
	m.memMutex.Unlock()
	if !HasOn() {
		return
	}

	if err := m.locker.Unlock(ctx); err != nil {
		log.Warn(errors.As(err, m.locker.Key()))
	}
}
func (m *mutex) Close() {
	if !HasOn() {
		return
	}

	if err := m.sess.Close(); err != nil {
		log.Warn(errors.As(err, m.locker.Key()))
	}
}

// key mutex locker
func NewMutex(key string, opts ...concurrency.SessionOption) (Mutex, error) {
	if !HasOn() {
		// return memory lock
		return &mutex{}, nil
	}

	cli, err := Client()
	if err != nil {
		return nil, errors.As(err)
	}
	s1, err := concurrency.NewSession(cli, opts...)
	if err != nil {
		return nil, errors.As(err)
	}
	return &mutex{
		sess:   s1,
		locker: concurrency.NewMutex(s1, fmt.Sprintf("/%s/", key)),
	}, nil
}
