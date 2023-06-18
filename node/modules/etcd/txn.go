package etcd

import clientv3 "go.etcd.io/etcd/client/v3"

type Txn struct {
	tx clientv3.Txn
}

func (txn *Txn) If(cs ...clientv3.Cmp) *Txn {
	return &Txn{tx: txn.tx.If(cs...)}
}

func (txn *Txn) Then(ops ...clientv3.Op) *Txn {
	return &Txn{tx: txn.tx.Then(ops...)}
}

func (txn *Txn) Else(ops ...clientv3.Op) *Txn {
	return &Txn{tx: txn.tx.Else(ops...)}
}
