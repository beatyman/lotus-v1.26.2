package etcd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMutex(t *testing.T) {
	// create two separate sessions for lock competition
	key := uuid.New().String()
	m1, err := GetMutex(key)
	if err != nil {
		t.Fatal(err)
	}
	defer m1.Close()

	//key = uuid.New().String()
	m2, err := GetMutex(key)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()

	buffMu := sync.Mutex{}
	buff := strings.Builder{}

	// acquire lock for s1
	if err := m1.Lock(context.TODO()); err != nil {
		t.Fatal(err)
	}
	buff.WriteString(fmt.Sprintf("acquired lock for m1\n"))

	m2Locked := make(chan struct{})
	go func() {
		defer close(m2Locked)
		// wait until m1 is locks
		if err := m2.Lock(context.TODO()); err != nil {
			t.Fatal(err)
		}
		buffMu.Lock()
		buff.WriteString(fmt.Sprintf("acquired lock for m2\n"))
		buffMu.Unlock()
	}()

	go func() {
		time.Sleep(1e9)
		if err := m1.Unlock(context.TODO()); err != nil {
			t.Fatal(err)
		}
		buffMu.Lock()
		buff.WriteString(fmt.Sprintf("released lock for m1\n"))
		buffMu.Unlock()
	}()

	go func() {
		<-m2Locked
		if err := m2.Unlock(context.TODO()); err != nil {
			t.Fatal(err)
		}
		buffMu.Lock()
		buff.WriteString(fmt.Sprintf("released lock for m2\n"))
		buffMu.Unlock()
	}()

	// waitting output
	time.Sleep(2e9)
	buffMu.Lock()
	fmt.Print(buff.String())
	buffMu.Unlock()

	// Output:
	// acquired lock for m1
	// released lock for m1
	// acquired lock for m2
	// released lock for m2
}
