package etcd

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// need run a ectd server on :2379
func TestEtcd(t *testing.T) {
	// test connection
	_, err := Client()
	if err != nil {
		t.Fatal(err)
	}
	closeClient()

	ctx := context.TODO()
	random := uuid.New().String()
	_, err = Put(ctx, "testing", random)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := Get(ctx, "testing")
	if err != nil {
		t.Fatal(err)
	}
	vals := resp.Kvs
	if len(vals) != 1 {
		t.Fatalf("expect 1 value, src:%+v", resp)
	}
	if string(vals[0].Value) != random {
		t.Fatalf("result not match:%s,%s", vals[0], random)
	}

	// TODO: Cmpact
	// TODO: Do

	// TODO:Txn
}
