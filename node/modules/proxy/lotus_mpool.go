package proxy

import (
	"context"
	"sort"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/gwaylib/errors"
)

func lotusMpoolStat(ctx context.Context, nApi api.FullNode) ([]api.ProxyMpStat, error) {
	filter := map[address.Address]struct{}{}
	addrss, err := nApi.WalletList(ctx)
	if err != nil {
		return nil, errors.As(err)
	}
	for _, a := range addrss {
		filter[a] = struct{}{}
	}

	ts, err := nApi.ChainHead(ctx)
	if err != nil {
		return nil, errors.As(err)
	}
	msgs, err := nApi.MpoolPending(ctx, types.EmptyTSK)
	if err != nil {
		return nil, errors.As(err)
	}
	type statBucket struct {
		msgs map[uint64]*types.SignedMessage
	}
	type mpStat struct {
		addr                 string
		past, cur, future    uint64
		belowCurr, belowPast uint64
		gasLimit             big.Int
	}

	buckets := map[address.Address]*statBucket{}
	for _, v := range msgs {
		if filter != nil {
			if _, has := filter[v.Message.From]; !has {
				continue
			}
		}

		bkt, ok := buckets[v.Message.From]
		if !ok {
			bkt = &statBucket{
				msgs: map[uint64]*types.SignedMessage{},
			}
			buckets[v.Message.From] = bkt
		}

		bkt.msgs[v.Message.Nonce] = v
	}

	var out []api.ProxyMpStat

	for a, bkt := range buckets {
		act, err := nApi.StateGetActor(ctx, a, ts.Key())
		if err != nil {
			return nil, errors.As(err)
		}

		cur := act.Nonce
		for {
			_, ok := bkt.msgs[cur]
			if !ok {
				break
			}
			cur++
		}

		var s api.ProxyMpStat
		s.Addr = a.String()
		s.GasLimit = big.Zero()

		for _, m := range bkt.msgs {
			if m.Message.Nonce < act.Nonce {
				s.Past++
			} else if m.Message.Nonce > cur {
				s.Future++
			} else {
				s.Cur++
			}

			s.GasLimit = big.Add(s.GasLimit, types.NewInt(uint64(m.Message.GasLimit)))
		}

		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Addr < out[j].Addr
	})
	return out, nil
}
