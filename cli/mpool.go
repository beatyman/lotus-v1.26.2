package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/chain/types"
)

var mpoolCmd = &cli.Command{
	Name:  "mpool",
	Usage: "Manage message pool",
	Subcommands: []*cli.Command{
		mpoolGetCfg,
		mpoolSetCfg,
		mpoolFix,
		mpoolPending,
		mpoolSub,
		mpoolStat,
		mpoolReplaceCmd,
		mpoolFindCmd,
	},
}
var mpoolGetCfg = &cli.Command{
	Name:  "get-cfg",
	Usage: "Println the configration of mpool",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)
		cfg, err := api.MpoolGetConfig(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%+v\n", cfg)
		return nil
	},
}
var mpoolSetCfg = &cli.Command{
	Name:  "set-cfg",
	Usage: "Println the configration of mpool",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "PriorityAddrs",
			Usage: "Array of address, split with ',', empty not change, '-' to clean.",
		},
		&cli.IntFlag{
			Name:  "SizeLimitHigh",
			Usage: "SizeLimitHigh, < 0 not change.",
			Value: -1,
		},
		&cli.IntFlag{
			Name:  "SizeLimitLow",
			Usage: "SizeLimitLow, < 0 not change.",
			Value: -1,
		},
		&cli.Float64Flag{
			Name:  "ReplaceByFeeRatio",
			Usage: "ReplaceByFeeRatio, < 0 not change.",
			Value: -1,
		},
		&cli.Int64Flag{
			Name:  "PruneCooldown",
			Usage: "PruneCooldown, < 0 not change.",
			Value: -1,
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)
		cfg, err := api.MpoolGetConfig(ctx)
		if err != nil {
			return err
		}
		coolDown := cctx.Int("PruneCooldown")
		if coolDown > -1 {
			cfg.PruneCooldown = time.Duration(coolDown)
		}
		radio := cctx.Float64("ReplaceByFeeRatio")
		if radio > -1 {
			cfg.ReplaceByFeeRatio = radio
		}

		limitLow := cctx.Int("SizeLimitLow")
		if limitLow > -1 {
			cfg.SizeLimitLow = limitLow
		}
		limitHigh := cctx.Int("SizeLimitHigh")
		if limitHigh > -1 {
			cfg.SizeLimitHigh = limitHigh
		}
		addrs := cctx.String("PriorityAddrs")
		if len(addrs) > 0 {
			tAddrs := []address.Address{}
			arrAddr := strings.Split(addrs, ",")
			for _, a := range arrAddr {
				if a == "-" {
					break
				}
				tAddr, err := address.NewFromString(a)
				if err != nil {
					return err
				}
				tAddrs = append(tAddrs, tAddr)
			}
			cfg.PriorityAddrs = tAddrs
		}
		if err := api.MpoolSetConfig(ctx, cfg); err != nil {
			return err
		}
		fmt.Printf("new cfg: %+v\n", cfg)
		return nil
	},
}

var mpoolFix = &cli.Command{
	Name:  "fix",
	Usage: "fix local message with hard code, the logic need to see the source code",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "address",
			Usage: "select a wallet address to fix.",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		fixAddr, err := address.NewFromString(cctx.String("address"))
		if err != nil {
			return errors.New("need input address")
		}
		filter := map[address.Address]struct{}{
			fixAddr: struct{}{},
		}

		msgs, err := api.MpoolPending(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}

		for _, msg := range msgs {
			if _, has := filter[msg.Message.From]; !has {
				continue
			}
			// fix with replace
			newMsg := msg.Message
			newMsg.GasPremium = types.BigAdd(
				newMsg.GasPremium,
				types.BigDiv(types.BigMul(newMsg.GasPremium, types.NewInt(50)), types.NewInt(100)),
			)
			newMsg.GasFeeCap = types.NewInt(1e11)

			smsg, err := api.WalletSignMessage(ctx, newMsg.From, &newMsg)
			if err != nil {
				return fmt.Errorf("failed to sign message: %w", err)
			}
			cid, err := api.MpoolPush(ctx, smsg)
			if err != nil {
				return fmt.Errorf("failed to push new message to mempool: %w", err)
			}
			fmt.Println("new message cid: ", cid)
		}

		return nil
	},
}
var mpoolPending = &cli.Command{
	Name:  "pending",
	Usage: "Get pending messages",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "local",
			Usage: "print pending messages for addresses in local wallet only",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		var filter map[address.Address]struct{}
		if cctx.Bool("local") {
			filter = map[address.Address]struct{}{}

			addrss, err := api.WalletList(ctx)
			if err != nil {
				return xerrors.Errorf("getting local addresses: %w", err)
			}

			for _, a := range addrss {
				filter[a] = struct{}{}
			}
		}

		msgs, err := api.MpoolPending(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}

		for _, msg := range msgs {
			if filter != nil {
				if _, has := filter[msg.Message.From]; !has {
					continue
				}
			}

			out, err := json.MarshalIndent(msg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		}

		return nil
	},
}

var mpoolSub = &cli.Command{
	Name:  "sub",
	Usage: "Subscribe to mpool changes",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		sub, err := api.MpoolSub(ctx)
		if err != nil {
			return err
		}

		for {
			select {
			case update := <-sub:
				out, err := json.MarshalIndent(update, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(out))
			case <-ctx.Done():
				return nil
			}
		}
	},
}

type statBucket struct {
	msgs map[uint64]*types.SignedMessage
}
type mpStat struct {
	addr              string
	past, cur, future uint64
}

var mpoolStat = &cli.Command{
	Name:  "stat",
	Usage: "print mempool stats",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "local",
			Usage: "print stats for addresses in local wallet only",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		ts, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting chain head: %w", err)
		}

		var filter map[address.Address]struct{}
		if cctx.Bool("local") {
			filter = map[address.Address]struct{}{}

			addrss, err := api.WalletList(ctx)
			if err != nil {
				return xerrors.Errorf("getting local addresses: %w", err)
			}

			for _, a := range addrss {
				filter[a] = struct{}{}
			}
		}

		msgs, err := api.MpoolPending(ctx, types.EmptyTSK)
		if err != nil {
			return err
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

		var out []mpStat

		for a, bkt := range buckets {
			act, err := api.StateGetActor(ctx, a, ts.Key())
			if err != nil {
				fmt.Printf("%s, err: %s\n", a, err)
				continue
			}

			cur := act.Nonce
			for {
				_, ok := bkt.msgs[cur]
				if !ok {
					break
				}
				cur++
			}

			past := uint64(0)
			future := uint64(0)
			for _, m := range bkt.msgs {
				if m.Message.Nonce < act.Nonce {
					past++
				}
				if m.Message.Nonce > cur {
					future++
				}
			}

			out = append(out, mpStat{
				addr:   a.String(),
				past:   past,
				cur:    cur - act.Nonce,
				future: future,
			})
		}

		sort.Slice(out, func(i, j int) bool {
			return out[i].addr < out[j].addr
		})

		var total mpStat

		for _, stat := range out {
			total.past += stat.past
			total.cur += stat.cur
			total.future += stat.future

			fmt.Printf("%s: past: %d, cur: %d, future: %d\n", stat.addr, stat.past, stat.cur, stat.future)
		}

		fmt.Println("-----")
		fmt.Printf("total: past: %d, cur: %d, future: %d\n", total.past, total.cur, total.future)

		return nil
	},
}

var mpoolReplaceCmd = &cli.Command{
	Name:  "replace",
	Usage: "replace a message in the mempool",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "gas-feecap",
			Usage: "gas feecap for new message",
		},
		&cli.StringFlag{
			Name:  "gas-premium",
			Usage: "gas price for new message",
		},
		&cli.Int64Flag{
			Name:  "gas-limit",
			Usage: "gas price for new message",
		},
	},
	ArgsUsage: "[from] [nonce]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 2 {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		from, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return err
		}

		nonce, err := strconv.ParseUint(cctx.Args().Get(1), 10, 64)
		if err != nil {
			return err
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		ts, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting chain head: %w", err)
		}

		pending, err := api.MpoolPending(ctx, ts.Key())
		if err != nil {
			return err
		}

		var found *types.SignedMessage
		for _, p := range pending {
			if p.Message.From == from && p.Message.Nonce == nonce {
				found = p
				break
			}
		}

		if found == nil {
			return fmt.Errorf("no pending message found from %s with nonce %d", from, nonce)
		}

		msg := found.Message

		msg.GasLimit = cctx.Int64("gas-limit")
		msg.GasPremium, err = types.BigFromString(cctx.String("gas-premium"))
		if err != nil {
			return fmt.Errorf("parsing gas-premium: %w", err)
		}
		// TODO: estiamte fee cap here
		msg.GasFeeCap, err = types.BigFromString(cctx.String("gas-feecap"))
		if err != nil {
			return fmt.Errorf("parsing gas-feecap: %w", err)
		}

		smsg, err := api.WalletSignMessage(ctx, msg.From, &msg)
		if err != nil {
			return fmt.Errorf("failed to sign message: %w", err)
		}

		cid, err := api.MpoolPush(ctx, smsg)
		if err != nil {
			return fmt.Errorf("failed to push new message to mempool: %w", err)
		}

		fmt.Println("new message cid: ", cid)
		return nil
	},
}

var mpoolFindCmd = &cli.Command{
	Name:  "find",
	Usage: "find a message in the mempool",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "search for messages with given 'from' address",
		},
		&cli.StringFlag{
			Name:  "to",
			Usage: "search for messages with given 'to' address",
		},
		&cli.Int64Flag{
			Name:  "method",
			Usage: "search for messages with given method",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		pending, err := api.MpoolPending(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}

		var toFilter, fromFilter address.Address
		if cctx.IsSet("to") {
			a, err := address.NewFromString(cctx.String("to"))
			if err != nil {
				return fmt.Errorf("'to' address was invalid: %w", err)
			}

			toFilter = a
		}

		if cctx.IsSet("from") {
			a, err := address.NewFromString(cctx.String("from"))
			if err != nil {
				return fmt.Errorf("'from' address was invalid: %w", err)
			}

			fromFilter = a
		}

		var methodFilter *abi.MethodNum
		if cctx.IsSet("method") {
			m := abi.MethodNum(cctx.Int64("method"))
			methodFilter = &m
		}

		var out []*types.SignedMessage
		for _, m := range pending {
			if toFilter != address.Undef && m.Message.To != toFilter {
				continue
			}

			if fromFilter != address.Undef && m.Message.From != fromFilter {
				continue
			}

			if methodFilter != nil && *methodFilter != m.Message.Method {
				continue
			}

			out = append(out, m)
		}

		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}
