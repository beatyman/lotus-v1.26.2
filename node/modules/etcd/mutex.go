package etcd

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"go.etcd.io/etcd/clientv3/concurrency"
)

type Mutex struct {
	mutex sync.Mutex
	sess  *concurrency.Session
	lock  *concurrency.Mutex
}

func (m *Mutex) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.sess == nil {
		return nil
	}

	return m.sess.Close()
}

func (m *Mutex) Lock(ctx context.Context) error {
	cli, err := Client()
	if err != nil {
		return errors.As(err)
	}

	m.mutex.Lock()
	if m.sess == nil {
		s1, err := concurrency.NewSession(cli)
		if err != nil {
			m.mutex.Unlock()
			closeClient()
			return errors.As(err)
		}
		m.sess = s1
	}
	if m.lock == nil {
		m.lock = concurrency.NewMutex(m.sess, fmt.Sprintf("/%s/", uuid.New().String()))
	}
	m.mutex.Unlock()

	return m.lock.Lock(ctx)
}
func (m *Mutex) Unlock(ctx context.Context) error {
	m.mutex.Lock()
	if m.sess == nil {
		m.mutex.Unlock()
		return nil
	}
	if m.lock == nil {
		m.mutex.Unlock()
		return nil
	}
	m.mutex.Unlock()

	if err := m.lock.Unlock(ctx); err != nil {
		closeClient()
		return err
	}
	return nil
}
