package etcd

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestMutex(t *testing.T) {
	// create two separate sessions for lock competition
	m1 := Mutex{}
	defer m1.Close()

	m2 := Mutex{}
	defer m2.Close()

	// acquire lock for s1
	if err := m1.Lock(context.TODO()); err != nil {
		t.Fatal(err)
	}
	fmt.Println("acquired lock for m1")

	m2Locked := make(chan struct{})
	go func() {
		defer close(m2Locked)
		// wait until m1 is locks
		fmt.Println("acquired lock for m2")
		if err := m2.Lock(context.TODO()); err != nil {
			t.Fatal(err)
		}
	}()

	time.Sleep(1e9)
	if err := m1.Unlock(context.TODO()); err != nil {
		t.Fatal(err)
	}
	fmt.Println("released lock for m1")

	<-m2Locked
	fmt.Println("released lock for m2")
	if err := m2.Unlock(context.TODO()); err != nil {
		t.Fatal(err)
	}

	// Output:
	// acquired lock for m1
	// acquired lock for m2
	// released lock for m1
	// released lock for m2
}
