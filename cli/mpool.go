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
		mpoolClear,
		mpoolSub,
		mpoolStat,
		mpoolReplaceCmd,
		mpoolFindCmd,
		mpoolConfig,
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
	Usage: "fix [address]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "fix local message with hard code, the logic need to see the source code",
		},
		&cli.Uint64Flag{
			Name:  "rate",
			Usage: "0<rate, will be divide 100",
			Value: 50,
		},
		&cli.Uint64Flag{
			Name:  "nonce",
			Usage: "message nonce. 0 is ignored",
			Value: 0,
		},
		&cli.Uint64Flag{
			Name:  "limit-msg",
			Usage: "limit the message. 0 is ignored",
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "limit-gas",
			Usage: "limit the gas. default is 100FIL in max",
			Value: "1000000000000000000000",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 1 {
			return errors.New("need input from address argument")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		fixAddr, err := address.NewFromString(cctx.Args().First())
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
		baseFee, err := api.ChainComputeBaseFee(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}
		limitGas, err := types.BigFromString(cctx.String("limit-gas"))
		if err != nil {
			return err
		}
		limitMsg := cctx.Uint64("limit-msg")
		nonce := cctx.Uint64("nonce")
		rate := cctx.Uint64("rate")

		fixedNum := uint64(0)
		gasUsed := types.NewInt(0)
		for idx, msg := range msgs {
			if _, has := filter[msg.Message.From]; !has {
				continue
			}
			if nonce > 0 && nonce != msg.Message.Nonce {
				continue
			}
			if limitMsg > 0 && fixedNum > limitMsg {
				continue
			}
			fixedNum++

			// fix with replace
			newMsg := msg.Message
			newMsg.GasPremium = types.BigAdd(
				newMsg.GasPremium,
				types.BigDiv(types.BigMul(newMsg.GasPremium, types.NewInt(rate)), types.NewInt(100)),
			)
			newMsg.GasFeeCap = types.BigAdd(
				baseFee,
				types.BigDiv(types.BigMul(baseFee, types.NewInt(rate)), types.NewInt(100)),
			)
			// ERROR: failed to push new message to mempool: message will not be included in a block: 'GasFeeCap' less than 'GasPremium'
			if types.BigCmp(newMsg.GasFeeCap, newMsg.GasPremium) < 0 {
				newMsg.GasFeeCap = newMsg.GasPremium
			}
			gasUsed = types.BigAdd(gasUsed, types.BigMul(newMsg.GasFeeCap, types.NewInt(uint64(newMsg.GasLimit))))
			if types.BigCmp(gasUsed, limitGas) >= 0 {
				return fmt.Errorf("gas out of limit: base:%s,used:%s", baseFee, gasUsed)
			}

			smsg, err := api.WalletSignMessage(ctx, newMsg.From, &newMsg)
			if err != nil {
				return fmt.Errorf("failed to sign message: %w", err)
			}
			cid, err := api.MpoolPush(ctx, smsg)
			if err != nil {
				return fmt.Errorf("failed to push new message to mempool: %w", err)
			}
			fmt.Printf("idx:%d, newCid:%s, newMsg:%s,oldMsg:%s \n", idx, cid, newMsg.String(), msg.Message.String())
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

var mpoolClear = &cli.Command{
	Name:  "clear",
	Usage: "Clear all pending messages from the mpool (USE WITH CARE)",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "local",
			Usage: "also clear local messages",
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "must be specified for the action to take effect",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		really := cctx.Bool("really-do-it")
		if !really {
			//nolint:golint
			return fmt.Errorf("--really-do-it must be specified for this action to have an effect; you have been warned")
		}

		local := cctx.Bool("local")

		ctx := ReqContext(cctx)
		return api.MpoolClear(ctx, local)
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
	addr                             string
	past, cur, future                uint64
	pastNonce, curNonce, futureNonce uint64
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
			pastNonce := uint64(0)
			future := uint64(0)
			futureNonce := uint64(0)
			for _, m := range bkt.msgs {
				if m.Message.Nonce < act.Nonce {
					past++
					if pastNonce > m.Message.Nonce || pastNonce == 0 {
						// get the min
						pastNonce = m.Message.Nonce
					}
				}
				if m.Message.Nonce > cur {
					future++
					if futureNonce > m.Message.Nonce || futureNonce == 0 {
						// get the min
						futureNonce = m.Message.Nonce
					}
				}
			}

			out = append(out, mpStat{
				addr:        a.String(),
				past:        past,
				cur:         cur - act.Nonce,
				future:      future,
				pastNonce:   pastNonce,
				curNonce:    act.Nonce,
				futureNonce: futureNonce,
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

			fmt.Printf("%s: past(%d): %d, cur(%d): %d, future(%d): %d\n",
				stat.addr, stat.pastNonce, stat.past, stat.curNonce, stat.cur, stat.futureNonce, stat.future,
			)
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
		// TODO: estimate fee cap here
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

var mpoolConfig = &cli.Command{
	Name:      "config",
	Usage:     "get or set current mpool configuration",
	ArgsUsage: "[new-config]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() > 1 {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		if cctx.Args().Len() == 0 {
			cfg, err := api.MpoolGetConfig(ctx)
			if err != nil {
				return err
			}

			bytes, err := json.Marshal(cfg)
			if err != nil {
				return err
			}

			fmt.Println(string(bytes))
		} else {
			cfg := new(types.MpoolConfig)
			bytes := []byte(cctx.Args().Get(0))

			err := json.Unmarshal(bytes, cfg)
			if err != nil {
				return err
			}

			return api.MpoolSetConfig(ctx, cfg)
		}

		return nil
	},
}
