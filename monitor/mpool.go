package monitor

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace/spans"
	"time"
)

func StartMPoolMonitor(api api.FullNode) {
	go mpoolMonitorLoop(api)
}

func mpoolMonitorLoop(api api.FullNode) {
	for {
		<-time.After(time.Minute * 3)

		addrs, err := api.WalletList(context.Background())
		if err != nil {
			continue
		}

		stats, err := mpoolStats(api, func(from, to string) bool {
			for _, addr := range addrs {
				if addr.String() == from {
					return true
				}
			}
			return false
		})
		if err != nil {
			continue
		}

		info := &model.MPoolInfo{
			Total: &model.MPoolStat{},
			Stats: stats,
		}
		for _, addr := range addrs {
			info.WalletAddrs = append(info.WalletAddrs, addr.String())
		}
		for _, stat := range stats {
			info.Total.Past += stat.Past
			info.Total.Cur += stat.Cur
			info.Total.Future += stat.Future

			info.Total.GasLimit += stat.GasLimit
			info.Total.BelowCurrBF += stat.BelowCurrBF
		}

		_, span := spans.NewMPoolSpan(context.Background())
		span.SetInfo(toJson(info))
		span.End()
	}
}

func mpoolStats(api api.FullNode, filter func(from, to string) bool) (map[string]*model.MPoolStat, error) {
	var (
		err    error
		ts     *types.TipSet
		currBF abi.TokenAmount
		msgs   []*types.SignedMessage
		ctx    = context.Background()
		grp    = make(map[address.Address]map[uint64]types.Message)
		out    = make(map[string]*model.MPoolStat)
	)
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("mpoolStats panic: %v", e)
		}
	}()

	if ts, err = api.ChainHead(ctx); err != nil {
		return nil, err
	} else {
		currBF = ts.Blocks()[0].ParentBaseFee
	}

	if msgs, err = api.MpoolPending(ctx, types.EmptyTSK); err != nil {
		return nil, err
	}

	for _, msg := range msgs {
		var (
			m = msg.Message
			f = m.From.String()
			t = m.To.String()
		)
		if filter != nil && !filter(f, t) {
			continue
		}

		if dict, ok := grp[m.From]; !ok {
			dict = make(map[uint64]types.Message)
			grp[m.From] = dict
		} else {
			dict[m.Nonce] = m
		}
	}

	for addr, dict := range grp {
		act, err := api.StateGetActor(ctx, addr, ts.Key())
		if err != nil {
			continue
		}

		cur := act.Nonce
		for {
			_, ok := dict[cur]
			if !ok {
				break
			}
			cur++
		}

		info := &model.MPoolStat{}
		gasLimit := big.Zero()
		for _, m := range dict {
			if m.Nonce < act.Nonce {
				info.Past++
			} else if m.Nonce > cur {
				info.Future++
			} else {
				info.Cur++
			}

			if m.GasFeeCap.LessThan(currBF) {
				info.BelowCurrBF++
			}

			gasLimit = big.Add(gasLimit, types.NewInt(uint64(m.GasLimit)))
		}
		info.GasLimit = gasLimit.Int64()
		out[addr.String()] = info
	}

	return out, nil
}
