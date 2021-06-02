package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	cbor "github.com/ipfs/go-ipld-cbor"

	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	sealing "github.com/filecoin-project/lotus/extern/storage-sealing"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
)

var infoCmd = &cli.Command{
	Name:  "info",
	Usage: "Print miner info",
	Subcommands: []*cli.Command{
		infoAllCmd,
	},
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "hide-sectors-info",
			Usage: "hide sectors info",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "seal",
			Value: false,
			Usage: "output the miner seal stats",
		},
	},
	Action: infoCmdAct,
}

func infoCmdAct(cctx *cli.Context) error {
	color.NoColor = !cctx.Bool("color")

	nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
	if err != nil {
		return err
	}
	defer closer()

	api, acloser, err := lcli.GetFullNodeAPI(cctx)
	if err != nil {
		return err
	}
	defer acloser()

	ctx := lcli.ReqContext(cctx)

	fmt.Print("Chain: ")

	head, err := api.ChainHead(ctx)
	if err != nil {
		return err
	}

	switch {
	case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*3/2): // within 1.5 epochs
		fmt.Printf("[%s]", color.GreenString("sync ok"))
	case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*5): // within 5 epochs
		fmt.Printf("[%s]", color.YellowString("sync slow (%s behind)", time.Now().Sub(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second)))
	default:
		fmt.Printf("[%s]", color.RedString("sync behind! (%s behind)", time.Now().Sub(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second)))
	}

	basefee := head.MinTicketBlock().ParentBaseFee
	gasCol := []color.Attribute{color.FgBlue}
	switch {
	case basefee.GreaterThan(big.NewInt(7000_000_000)): // 7 nFIL
		gasCol = []color.Attribute{color.BgRed, color.FgBlack}
	case basefee.GreaterThan(big.NewInt(3000_000_000)): // 3 nFIL
		gasCol = []color.Attribute{color.FgRed}
	case basefee.GreaterThan(big.NewInt(750_000_000)): // 750 uFIL
		gasCol = []color.Attribute{color.FgYellow}
	case basefee.GreaterThan(big.NewInt(100_000_000)): // 100 uFIL
		gasCol = []color.Attribute{color.FgGreen}
	}
	fmt.Printf(" [basefee %s]", color.New(gasCol...).Sprint(types.FIL(basefee).Short()))

	fmt.Println()

	maddr, err := getActorAddress(ctx, cctx)
	if err != nil {
		return err
	}

	mact, err := api.StateGetActor(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return err
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(api), blockstore.NewMemory())
	mas, err := miner.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), mact)
	if err != nil {
		return err
	}

	// Sector size
	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return err
	}

	ssize := types.SizeStr(types.NewInt(uint64(mi.SectorSize)))
	fmt.Printf("Miner: %s (%s sectors)\n", color.BlueString("%s", maddr), ssize)

	pow, err := api.StateMinerPower(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return err
	}

	rpercI := types.BigDiv(types.BigMul(pow.MinerPower.RawBytePower, types.NewInt(1000000)), pow.TotalPower.RawBytePower)
	qpercI := types.BigDiv(types.BigMul(pow.MinerPower.QualityAdjPower, types.NewInt(1000000)), pow.TotalPower.QualityAdjPower)

	fmt.Printf("Power: %s / %s (%0.4f%%)\n",
		color.GreenString(types.DeciStr(pow.MinerPower.QualityAdjPower)),
		types.DeciStr(pow.TotalPower.QualityAdjPower),
		float64(qpercI.Int64())/10000)

	fmt.Printf("\tRaw: %s / %s (%0.4f%%)\n",
		color.BlueString(types.SizeStr(pow.MinerPower.RawBytePower)),
		types.SizeStr(pow.TotalPower.RawBytePower),
		float64(rpercI.Int64())/10000)

	secCounts, err := api.StateMinerSectorCount(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return err
	}

	proving := secCounts.Active + secCounts.Faulty
	nfaults := secCounts.Faulty
	fmt.Printf("\tCommitted: %s\n", types.SizeStr(types.BigMul(types.NewInt(secCounts.Live), types.NewInt(uint64(mi.SectorSize)))))
	if nfaults == 0 {
		fmt.Printf("\tProving: %s\n", types.SizeStr(types.BigMul(types.NewInt(proving), types.NewInt(uint64(mi.SectorSize)))))
	} else {
		var faultyPercentage float64
		if secCounts.Live != 0 {
			faultyPercentage = float64(10000*nfaults/secCounts.Live) / 100.
		}
		fmt.Printf("\tProving: %s (%s Faulty, %.2f%%)\n",
			types.SizeStr(types.BigMul(types.NewInt(proving), types.NewInt(uint64(mi.SectorSize)))),
			types.SizeStr(types.BigMul(types.NewInt(nfaults), types.NewInt(uint64(mi.SectorSize)))),
			faultyPercentage)
	}

	if !pow.HasMinPower {
		fmt.Print("Below minimum power threshold, no blocks will be won\n")
	} else {
		expWinChance := float64(types.BigMul(qpercI, types.NewInt(build.BlocksPerEpoch)).Int64()) / 1000000
		//expWinChance := float64(qpercI.Int64()) / 1000000
		if expWinChance > 0 {
			if expWinChance > 1 {
				expWinChance = 1
			}
			winRate := time.Duration(float64(time.Second*time.Duration(build.BlockDelaySecs)) / expWinChance)
			winPerDay := float64(time.Hour*24) / float64(winRate)
			fmt.Printf("Expected block win rate(%.4f%%): ", expWinChance*100)
			color.Blue("%.4f blocks/day (every %s)", winPerDay, winRate.Truncate(time.Second))
			fmt.Println()

			limit := 7
			now := time.Unix(int64(head.MinTimestamp()), 0).UTC()
			statisWins, err := nodeApi.StatisWins(ctx, now, limit)
			if err != nil {
				fmt.Printf("Statis Win %s\n", errors.As(err).Code())
			} else if len(statisWins) == 0 {
				fmt.Printf("Statis Win %s\n", "No data found")
			} else {
				// for current
				statisWin := statisWins[0]
				beginDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
				rounds := now.Sub(beginDay) / (time.Second * time.Duration(build.BlockDelaySecs))
				if rounds > time.Duration(head.Height()) {
					rounds = time.Duration(head.Height())
				}
				expDayRounds := int(time.Hour * 24 / (time.Second * time.Duration(build.BlockDelaySecs)))
				expectNum := int(float64(rounds) * expWinChance)
				avgUsed := time.Duration(0)
				if statisWin.WinGen > 0 {
					avgUsed = time.Duration(statisWin.WinUsed / int64(statisWin.WinGen))
				}
				fmt.Printf(
					`Statis Win %s(UTC): 
	expect day:  rounds:%d, win:%d 
	expect cur:  rounds:%d, win:%d 
	actual run:  draw:%d, err:%d, win:%d, suc:%d, lost:%d,
	actual rate: draw rate:%.2f%%, win rate:%.2f%%, suc rate:%.2f%%
	average win took: %s
`,
					statisWin.Id,

					expDayRounds, statisWin.WinExp,
					rounds, expectNum,

					statisWin.WinAll, statisWin.WinErr, statisWin.WinGen,
					statisWin.WinSuc, statisWin.WinErr+(statisWin.WinGen-statisWin.WinSuc),

					float64(statisWin.WinAll*100)/float64(rounds), float64(statisWin.WinGen*100)/float64(expectNum), float64(statisWin.WinSuc*100)/float64(expectNum),

					avgUsed,
				)

				// sum for limit day
				sumExpRounds := expDayRounds * limit
				sumExpWin := 0
				sumDraw := 0
				sumErr := 0
				sumWin := 0
				sumSuc := 0
				for _, w := range statisWins {
					sumExpWin += w.WinExp
					sumDraw += w.WinAll
					sumErr += w.WinErr
					sumWin += w.WinGen
					sumSuc += w.WinSuc
				}
				fmt.Printf(
					`Statis %d days win:
	expect day:  rounds:%d, win:%d 
	actual run:  draw:%d, err:%d, win:%d, suc:%d, lost:%d,
	actual rate: draw rate:%.2f%%, win rate:%.2f%%, suc rate:%.2f%%
`,
					len(statisWins),
					sumExpRounds, sumExpWin,
					sumDraw, sumErr, sumWin, sumSuc, sumErr+sumWin-sumSuc,
					float64(sumDraw*100)/float64(sumExpRounds), float64(sumWin*100)/float64(sumExpWin), float64(sumSuc*100)/float64(sumExpWin),
				)
			}
		}
	}
	fmt.Println()

	deals, err := nodeApi.MarketListIncompleteDeals(ctx)
	if err != nil {
		return err
	}

	var nactiveDeals, nVerifDeals, ndeals uint64
	var activeDealBytes, activeVerifDealBytes, dealBytes abi.PaddedPieceSize
	for _, deal := range deals {
		if deal.State == storagemarket.StorageDealError {
			continue
		}

		ndeals++
		dealBytes += deal.Proposal.PieceSize

		if deal.State == storagemarket.StorageDealActive {
			nactiveDeals++
			activeDealBytes += deal.Proposal.PieceSize

			if deal.Proposal.VerifiedDeal {
				nVerifDeals++
				activeVerifDealBytes += deal.Proposal.PieceSize
			}
		}
	}

	fmt.Printf("Deals: %d, %s\n", ndeals, types.SizeStr(types.NewInt(uint64(dealBytes))))
	fmt.Printf("\tActive: %d, %s (Verified: %d, %s)\n", nactiveDeals, types.SizeStr(types.NewInt(uint64(activeDealBytes))), nVerifDeals, types.SizeStr(types.NewInt(uint64(activeVerifDealBytes))))
	fmt.Println()

	spendable := big.Zero()

	// NOTE: there's no need to unlock anything here. Funds only
	// vest on deadline boundaries, and they're unlocked by cron.
	lockedFunds, err := mas.LockedFunds()
	if err != nil {
		return xerrors.Errorf("getting locked funds: %w", err)
	}
	availBalance, err := mas.AvailableBalance(mact.Balance)
	if err != nil {
		return xerrors.Errorf("getting available balance: %w", err)
	}
	spendable = big.Add(spendable, availBalance)

	fmt.Printf("Miner Balance:    %s\n", color.YellowString("%s", types.FIL(mact.Balance).Short()))
	fmt.Printf("      PreCommit:  %s\n", types.FIL(lockedFunds.PreCommitDeposits).Short())
	fmt.Printf("      Pledge:     %s\n", types.FIL(lockedFunds.InitialPledgeRequirement).Short())
	fmt.Printf("      Vesting:    %s\n", types.FIL(lockedFunds.VestingFunds).Short())
	colorTokenAmount("      Available:  %s\n", availBalance)

	mb, err := api.StateMarketBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting market balance: %w", err)
	}
	spendable = big.Add(spendable, big.Sub(mb.Escrow, mb.Locked))

	fmt.Printf("Market Balance:   %s\n", types.FIL(mb.Escrow).Short())
	fmt.Printf("       Locked:    %s\n", types.FIL(mb.Locked).Short())
	colorTokenAmount("       Available: %s\n", big.Sub(mb.Escrow, mb.Locked))

	wb, err := api.WalletBalance(ctx, mi.Worker)
	if err != nil {
		return xerrors.Errorf("getting worker balance: %w", err)
	}
	spendable = big.Add(spendable, wb)
	color.Cyan("Worker Balance:   %s", types.FIL(wb).Short())
	if len(mi.ControlAddresses) > 0 {
		cbsum := big.Zero()
		for _, ca := range mi.ControlAddresses {
			b, err := api.WalletBalance(ctx, ca)
			if err != nil {
				return xerrors.Errorf("getting control address balance: %w", err)
			}
			cbsum = big.Add(cbsum, b)
		}
		spendable = big.Add(spendable, cbsum)

		fmt.Printf("       Control:   %s\n", types.FIL(cbsum).Short())
	}
	colorTokenAmount("Total Spendable:  %s\n", spendable)

	fmt.Println()

	if !cctx.Bool("hide-sectors-info") || cctx.Bool("seal") {
		fmt.Println("Sectors:")
		err = sectorsInfo(ctx, nodeApi)
		if err != nil {
			return err
		}
	}

	// TODO: grab actr state / info
	//  * Sealed sectors (count / bytes)
	//  * Power
	return nil
}

type stateMeta struct {
	i     int
	col   color.Attribute
	state sealing.SectorState
}

var stateOrder = map[sealing.SectorState]stateMeta{}
var stateList = []stateMeta{
	{col: 39, state: "Total"},
	{col: color.FgGreen, state: sealing.Proving},

	{col: color.FgBlue, state: sealing.Empty},
	{col: color.FgBlue, state: sealing.WaitDeals},
	{col: color.FgBlue, state: sealing.AddPiece},

	{col: color.FgRed, state: sealing.UndefinedSectorState},
	{col: color.FgYellow, state: sealing.Packing},
	{col: color.FgYellow, state: sealing.GetTicket},
	{col: color.FgYellow, state: sealing.PreCommit1},
	{col: color.FgYellow, state: sealing.PreCommit2},
	{col: color.FgYellow, state: sealing.PreCommitting},
	{col: color.FgYellow, state: sealing.PreCommitWait},
	{col: color.FgYellow, state: sealing.WaitSeed},
	{col: color.FgYellow, state: sealing.Committing},
	{col: color.FgYellow, state: sealing.SubmitCommit},
	{col: color.FgYellow, state: sealing.CommitWait},
	{col: color.FgYellow, state: sealing.FinalizeSector},

	{col: color.FgCyan, state: sealing.Terminating},
	{col: color.FgCyan, state: sealing.TerminateWait},
	{col: color.FgCyan, state: sealing.TerminateFinality},
	{col: color.FgCyan, state: sealing.TerminateFailed},
	{col: color.FgCyan, state: sealing.Removing},
	{col: color.FgCyan, state: sealing.Removed},

	{col: color.FgRed, state: sealing.FailedUnrecoverable},
	{col: color.FgRed, state: sealing.AddPieceFailed},
	{col: color.FgRed, state: sealing.SealPreCommit1Failed},
	{col: color.FgRed, state: sealing.SealPreCommit2Failed},
	{col: color.FgRed, state: sealing.PreCommitFailed},
	{col: color.FgRed, state: sealing.ComputeProofFailed},
	{col: color.FgRed, state: sealing.CommitFailed},
	{col: color.FgRed, state: sealing.PackingFailed},
	{col: color.FgRed, state: sealing.FinalizeFailed},
	{col: color.FgRed, state: sealing.Faulty},
	{col: color.FgRed, state: sealing.FaultReported},
	{col: color.FgRed, state: sealing.FaultedFinal},
	{col: color.FgRed, state: sealing.RemoveFailed},
	{col: color.FgRed, state: sealing.DealsExpired},
	{col: color.FgRed, state: sealing.RecoverDealIDs},
}

func init() {
	for i, state := range stateList {
		stateOrder[state.state] = stateMeta{
			i:   i,
			col: state.col,
		}
	}
}

func sectorsInfo(ctx context.Context, napi api.StorageMiner) error {
	summary, err := napi.SectorsSummary(ctx)
	if err != nil {
		return err
	}

	buckets := make(map[sealing.SectorState]int)
	var total int
	for s, c := range summary {
		buckets[sealing.SectorState(s)] = c
		total += c
	}
	buckets["Total"] = total

	var sorted []stateMeta
	for state, i := range buckets {
		sorted = append(sorted, stateMeta{i: i, state: state})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return stateOrder[sorted[i].state].i < stateOrder[sorted[j].state].i
	})

	for _, s := range sorted {
		_, _ = color.New(stateOrder[s.state].col).Printf("\t%s: %d\n", s.state, s.i)
	}

	return nil
}

func colorTokenAmount(format string, amount abi.TokenAmount) {
	if amount.GreaterThan(big.Zero()) {
		color.Green(format, types.FIL(amount).Short())
	} else if amount.Equals(big.Zero()) {
		color.Yellow(format, types.FIL(amount).Short())
	} else {
		color.Red(format, types.FIL(amount).Short())
	}
}
