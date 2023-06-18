package main

import (
	"context"
	"fmt"
	"math"
	corebig "math/big"
	"os"
	"sort"
	"time"

	"github.com/fatih/color"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/actors/builtin"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/journal/alerting"
	sealing "github.com/filecoin-project/lotus/storage/pipeline"
	"github.com/filecoin-project/lotus/storage/sealer/sealtasks"
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
		&cli.IntFlag{
			Name:  "statis-win-limit",
			Value: 7,
			Usage: "limit of win statistics",
		},
		&cli.IntFlag{
			Name:  "blocks",
			Usage: "Log of produced <blocks> newest blocks and rewards(Miner Fee excluded)",
		},
	},
	Action: infoCmdAct,
}

func infoCmdAct(cctx *cli.Context) error {
	minerApi, closer, err := lcli.GetStorageMinerAPI(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fullapi, acloser, err := lcli.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer acloser()

	ctx := lcli.ReqContext(cctx)

	subsystems, err := minerApi.RuntimeSubsystems(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Enabled subsystems:", subsystems)

	start, err := minerApi.StartTime(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("StartTime: %s (started at %s)\n", time.Now().Sub(start).Truncate(time.Second), start.Truncate(time.Second))

	fmt.Print("Chain: ")

	err = lcli.SyncBasefeeCheck(ctx, fullapi)
	if err != nil {
		return err
	}

	fmt.Println()

	alerts, err := minerApi.LogAlerts(ctx)
	if err != nil {
		fmt.Printf("ERROR: getting alerts: %s\n", err)
	}

	activeAlerts := make([]alerting.Alert, 0)
	for _, alert := range alerts {
		if alert.Active {
			activeAlerts = append(activeAlerts, alert)
		}
	}
	if len(activeAlerts) > 0 {
		fmt.Printf("%s (check %s)\n", color.RedString("⚠ %d Active alerts", len(activeAlerts)), color.YellowString("lotus-miner log alerts"))
	}

	err = handleMiningInfo(ctx, cctx, fullapi, minerApi)
	if err != nil {
		return err
	}

	return nil
}

func handleMiningInfo(ctx context.Context, cctx *cli.Context, fullapi v1api.FullNode, nodeApi api.StorageMiner) error {
	maddr, err := getActorAddress(ctx, cctx)
	if err != nil {
		return err
	}

	mact, err := fullapi.StateGetActor(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return err
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(fullapi), blockstore.NewMemory())

	mas, err := miner.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), mact)
	if err != nil {
		return err
	}

	// Sector size
	mi, err := fullapi.StateMinerInfo(ctx, maddr, types.EmptyTSK)

	if err != nil {
		return err
	}

	ssize := types.SizeStr(types.NewInt(uint64(mi.SectorSize)))
	fmt.Printf("Miner: %s (%s sectors)\n", color.BlueString("%s", maddr), ssize)

	pow, err := fullapi.StateMinerPower(ctx, maddr, types.EmptyTSK)
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

	secCounts, err := fullapi.StateMinerSectorCount(ctx, maddr, types.EmptyTSK)

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
	head, err := fullapi.ChainHead(ctx)
	if err != nil {
		return err
	}
	if !pow.HasMinPower {
		fmt.Print("Below minimum power threshold, no blocks will be won\n")
	} else {
		winRatio := new(corebig.Rat).SetFrac(
			types.BigMul(pow.MinerPower.QualityAdjPower, types.NewInt(build.BlocksPerEpoch)).Int,
			pow.TotalPower.QualityAdjPower.Int,
		)

		if winRatioFloat, _ := winRatio.Float64(); winRatioFloat > 0 {

			// if the corresponding poisson distribution isn't infinitely small then
			// throw it into the mix as well, accounting for multi-wins
			winRationWithPoissonFloat := -math.Expm1(-winRatioFloat)
			winRationWithPoisson := new(corebig.Rat).SetFloat64(winRationWithPoissonFloat)
			if winRationWithPoisson != nil {
				winRatio = winRationWithPoisson
				winRatioFloat = winRationWithPoissonFloat
			}

			weekly, _ := new(corebig.Rat).Mul(
				winRatio,
				new(corebig.Rat).SetInt64(7*builtin.EpochsInDay),
			).Float64()

			avgDuration, _ := new(corebig.Rat).Mul(
				new(corebig.Rat).SetInt64(builtin.EpochDurationSeconds),
				new(corebig.Rat).Inv(winRatio),
			).Float64()

			fmt.Print("Projected average block win rate: ")
			color.Blue(
				"%.02f/week (every %s)",
				weekly,
				(time.Second * time.Duration(avgDuration)).Truncate(time.Second).String(),
			)

			// Geometric distribution of P(Y < k) calculated as described in https://en.wikipedia.org/wiki/Geometric_distribution#Probability_Outcomes_Examples
			// https://www.wolframalpha.com/input/?i=t+%3E+0%3B+p+%3E+0%3B+p+%3C+1%3B+c+%3E+0%3B+c+%3C1%3B+1-%281-p%29%5E%28t%29%3Dc%3B+solve+t
			// t == how many dice-rolls (epochs) before win
			// p == winRate == ( minerPower / netPower )
			// c == target probability of win ( 99.9% in this case )
			fmt.Print("Projected block win with ")
			color.Green(
				"99.9%% probability every %s",
				(time.Second * time.Duration(
					builtin.EpochDurationSeconds*math.Log(1-0.999)/
						math.Log(1-winRatioFloat),
				)).Truncate(time.Second).String(),
			)
			fmt.Println("(projections DO NOT account for future network and miner growth)")
		}
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

			limit := cctx.Int("statis-win-limit")
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
				pDrawRate := float64(0)
				if rounds > 0 {
					pDrawRate = float64(statisWin.WinAll*100) / float64(rounds)
				}
				pWinRate := float64(0)
				pSucRate := float64(0)
				if expectNum > 0 {
					pWinRate = float64(statisWin.WinGen*100) / float64(expectNum)
					pSucRate = float64(statisWin.WinSuc*100) / float64(expectNum)
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

					pDrawRate, pWinRate, pSucRate,

					avgUsed,
				)

				// sum for limit day
				sumExpRounds := expDayRounds*(len(statisWins)-1) + int(rounds)
				sumExpWin := expectNum
				sumDraw := statisWin.WinAll
				sumErr := statisWin.WinErr
				sumWin := statisWin.WinGen
				sumSuc := statisWin.WinSuc
				for i := 1; i < len(statisWins); i++ {
					w := statisWins[i]
					sumExpWin += w.WinExp
					sumDraw += w.WinAll
					sumErr += w.WinErr
					sumWin += w.WinGen
					sumSuc += w.WinSuc
				}
				sumDrawRate := float64(0)
				if sumExpRounds > 0 {
					sumDrawRate = float64(sumDraw*100) / float64(sumExpRounds)
				}
				sumWinRate := float64(0)
				if sumExpWin > 0 {
					sumWinRate = float64(sumWin*100) / float64(sumExpWin)
				}
				sumSucRate := float64(0)
				if sumExpWin > 0 {
					sumSucRate = float64(sumSuc*100) / float64(sumExpWin)
				}
				fmt.Printf(
					`Statis %d days win:
	expect all:  rounds:%d, win:%d 
	actual run:  draw:%d, err:%d, win:%d, suc:%d, lost:%d,
	actual rate: draw rate:%.2f%%, win rate:%.2f%%, suc rate:%.2f%%
`,
					len(statisWins),
					sumExpRounds, sumExpWin,
					sumDraw, sumErr, sumWin, sumSuc, sumErr+sumWin-sumSuc,
					sumDrawRate, sumWinRate, sumSucRate,
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

	if availBalance.GreaterThan(big.Zero()) {
		spendable = big.Add(spendable, availBalance)
	}

	fmt.Printf("Miner Balance:    %s\n", color.YellowString("%s", types.FIL(mact.Balance).Short()))
	fmt.Printf("      PreCommit:  %s\n", types.FIL(lockedFunds.PreCommitDeposits).Short())
	fmt.Printf("      Pledge:     %s\n", types.FIL(lockedFunds.InitialPledgeRequirement).Short())
	fmt.Printf("      Vesting:    %s\n", types.FIL(lockedFunds.VestingFunds).Short())
	colorTokenAmount("      Available:  %s\n", availBalance)

	mb, err := fullapi.StateMarketBalance(ctx, maddr, types.EmptyTSK)

	if err != nil {
		return xerrors.Errorf("getting market balance: %w", err)
	}
	spendable = big.Add(spendable, big.Sub(mb.Escrow, mb.Locked))

	fmt.Printf("Market Balance:   %s\n", types.FIL(mb.Escrow).Short())
	fmt.Printf("       Locked:    %s\n", types.FIL(mb.Locked).Short())
	colorTokenAmount("       Available: %s\n", big.Sub(mb.Escrow, mb.Locked))

	wb, err := fullapi.WalletBalance(ctx, mi.Worker)

	if err != nil {
		return xerrors.Errorf("getting worker balance: %w", err)
	}
	spendable = big.Add(spendable, wb)
	color.Cyan("Worker Balance:   %s", types.FIL(wb).Short())
	if len(mi.ControlAddresses) > 0 {
		cbsum := big.Zero()
		for _, ca := range mi.ControlAddresses {
			b, err := fullapi.WalletBalance(ctx, ca)
			if err != nil {
				return xerrors.Errorf("getting control address balance: %w", err)
			}
			cbsum = big.Add(cbsum, b)
		}
		spendable = big.Add(spendable, cbsum)

		fmt.Printf("       Control:   %s\n", types.FIL(cbsum).Short())
	}
	colorTokenAmount("Total Spendable:  %s\n", spendable)

	if mi.Beneficiary != address.Undef {
		fmt.Println()
		fmt.Printf("Beneficiary:\t%s\n", mi.Beneficiary)
		if mi.Beneficiary != mi.Owner {
			fmt.Printf("Beneficiary Quota:\t%s\n", mi.BeneficiaryTerm.Quota)
			fmt.Printf("Beneficiary Used Quota:\t%s\n", mi.BeneficiaryTerm.UsedQuota)
			fmt.Printf("Beneficiary Expiration:\t%s\n", mi.BeneficiaryTerm.Expiration)
		}
	}
	if mi.PendingBeneficiaryTerm != nil {
		fmt.Printf("Pending Beneficiary Term:\n")
		fmt.Printf("New Beneficiary:\t%s\n", mi.PendingBeneficiaryTerm.NewBeneficiary)
		fmt.Printf("New Quota:\t%s\n", mi.PendingBeneficiaryTerm.NewQuota)
		fmt.Printf("New Expiration:\t%s\n", mi.PendingBeneficiaryTerm.NewExpiration)
		fmt.Printf("Approved By Beneficiary:\t%t\n", mi.PendingBeneficiaryTerm.ApprovedByBeneficiary)
		fmt.Printf("Approved By Nominee:\t%t\n", mi.PendingBeneficiaryTerm.ApprovedByNominee)
	}
	fmt.Println()

	if !cctx.Bool("hide-sectors-info") || cctx.Bool("seal") {
		fmt.Println("Sectors:")
		err = sectorsInfo(ctx, nodeApi)
		if err != nil {
			return err
		}
	}

	{
		fmt.Println()

		ws, err := nodeApi.WorkerStats(ctx)
		if err != nil {
			return xerrors.Errorf("getting worker stats: %w", err)
		}

		workersByType := map[string]int{
			sealtasks.WorkerSealing:     0,
			sealtasks.WorkerWindowPoSt:  0,
			sealtasks.WorkerWinningPoSt: 0,
		}

	wloop:
		for _, st := range ws {
			if !st.Enabled {
				continue
			}

			for _, task := range st.Tasks {
				if task.WorkerType() != sealtasks.WorkerSealing {
					workersByType[task.WorkerType()]++
					continue wloop
				}
			}
			workersByType[sealtasks.WorkerSealing]++
		}

		fmt.Printf("Workers: Seal(%d) WdPoSt(%d) WinPoSt(%d)\n",
			workersByType[sealtasks.WorkerSealing],
			workersByType[sealtasks.WorkerWindowPoSt],
			workersByType[sealtasks.WorkerWinningPoSt])
	}

	if cctx.IsSet("blocks") {
		fmt.Println("Produced newest blocks:")
		err = producedBlocks(ctx, cctx.Int("blocks"), maddr, fullapi)
		if err != nil {
			return err
		}
	}

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
	{col: color.FgGreen, state: sealing.Available},
	{col: color.FgGreen, state: sealing.UpdateActivating},

	{col: color.FgMagenta, state: sealing.ReceiveSector},

	{col: color.FgBlue, state: sealing.Empty},
	{col: color.FgBlue, state: sealing.WaitDeals},
	{col: color.FgBlue, state: sealing.AddPiece},
	{col: color.FgBlue, state: sealing.SnapDealsWaitDeals},
	{col: color.FgBlue, state: sealing.SnapDealsAddPiece},

	{col: color.FgRed, state: sealing.UndefinedSectorState},
	{col: color.FgYellow, state: sealing.Packing},
	{col: color.FgYellow, state: sealing.GetTicket},
	{col: color.FgYellow, state: sealing.PreCommit1},
	{col: color.FgYellow, state: sealing.PreCommit2},
	{col: color.FgYellow, state: sealing.PreCommitting},
	{col: color.FgYellow, state: sealing.PreCommitWait},
	{col: color.FgYellow, state: sealing.SubmitPreCommitBatch},
	{col: color.FgYellow, state: sealing.PreCommitBatchWait},
	{col: color.FgYellow, state: sealing.WaitSeed},
	{col: color.FgYellow, state: sealing.Committing},
	{col: color.FgYellow, state: sealing.CommitFinalize},
	{col: color.FgYellow, state: sealing.SubmitCommit},
	{col: color.FgYellow, state: sealing.CommitWait},
	{col: color.FgYellow, state: sealing.SubmitCommitAggregate},
	{col: color.FgYellow, state: sealing.CommitAggregateWait},
	{col: color.FgYellow, state: sealing.FinalizeSector},
	{col: color.FgYellow, state: sealing.SnapDealsPacking},
	{col: color.FgYellow, state: sealing.UpdateReplica},
	{col: color.FgYellow, state: sealing.ProveReplicaUpdate},
	{col: color.FgYellow, state: sealing.SubmitReplicaUpdate},
	{col: color.FgYellow, state: sealing.ReplicaUpdateWait},
	{col: color.FgYellow, state: sealing.WaitMutable},
	{col: color.FgYellow, state: sealing.FinalizeReplicaUpdate},
	{col: color.FgYellow, state: sealing.ReleaseSectorKey},

	{col: color.FgCyan, state: sealing.Terminating},
	{col: color.FgCyan, state: sealing.TerminateWait},
	{col: color.FgCyan, state: sealing.TerminateFinality},
	{col: color.FgCyan, state: sealing.TerminateFailed},
	{col: color.FgCyan, state: sealing.Removing},
	{col: color.FgCyan, state: sealing.Removed},
	{col: color.FgCyan, state: sealing.AbortUpgrade},

	{col: color.FgRed, state: sealing.FailedUnrecoverable},
	{col: color.FgRed, state: sealing.AddPieceFailed},
	{col: color.FgRed, state: sealing.SealPreCommit1Failed},
	{col: color.FgRed, state: sealing.SealPreCommit2Failed},
	{col: color.FgRed, state: sealing.PreCommitFailed},
	{col: color.FgRed, state: sealing.ComputeProofFailed},
	{col: color.FgRed, state: sealing.RemoteCommitFailed},
	{col: color.FgRed, state: sealing.CommitFailed},
	{col: color.FgRed, state: sealing.CommitFinalizeFailed},
	{col: color.FgRed, state: sealing.PackingFailed},
	{col: color.FgRed, state: sealing.FinalizeFailed},
	{col: color.FgRed, state: sealing.Faulty},
	{col: color.FgRed, state: sealing.FaultReported},
	{col: color.FgRed, state: sealing.FaultedFinal},
	{col: color.FgRed, state: sealing.RemoveFailed},
	{col: color.FgRed, state: sealing.DealsExpired},
	{col: color.FgRed, state: sealing.RecoverDealIDs},
	{col: color.FgRed, state: sealing.SnapDealsAddPieceFailed},
	{col: color.FgRed, state: sealing.SnapDealsDealsExpired},
	{col: color.FgRed, state: sealing.ReplicaUpdateFailed},
	{col: color.FgRed, state: sealing.ReleaseSectorKeyFailed},
	{col: color.FgRed, state: sealing.FinalizeReplicaUpdateFailed},
}

func init() {
	for i, state := range stateList {
		stateOrder[state.state] = stateMeta{
			i:   i,
			col: state.col,
		}
	}
}

func sectorsInfo(ctx context.Context, mapi api.StorageMiner) error {
	summary, err := mapi.SectorsSummary(ctx)
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

func producedBlocks(ctx context.Context, count int, maddr address.Address, napi v1api.FullNode) error {
	var err error
	head, err := napi.ChainHead(ctx)
	if err != nil {
		return err
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(napi), blockstore.NewMemory())

	tty := isatty.IsTerminal(os.Stderr.Fd())

	ts := head
	fmt.Printf(" Epoch   | Block ID                                                       | Reward\n")
	for count > 0 {
		tsk := ts.Key()
		bhs := ts.Blocks()
		for _, bh := range bhs {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if bh.Miner == maddr {
				if tty {
					_, _ = fmt.Fprint(os.Stderr, "\r\x1b[0K")
				}

				rewardActor, err := napi.StateGetActor(ctx, reward.Address, tsk)
				if err != nil {
					return err
				}

				rewardActorState, err := reward.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), rewardActor)
				if err != nil {
					return err
				}
				blockReward, err := rewardActorState.ThisEpochReward()
				if err != nil {
					return err
				}

				minerReward := types.BigDiv(types.BigMul(types.NewInt(uint64(bh.ElectionProof.WinCount)),
					blockReward), types.NewInt(uint64(builtin.ExpectedLeadersPerEpoch)))

				fmt.Printf("%8d | %s | %s\n", ts.Height(), bh.Cid(), types.FIL(minerReward))
				count--
			} else if tty && bh.Height%120 == 0 {
				_, _ = fmt.Fprintf(os.Stderr, "\r\x1b[0KChecking epoch %s", cliutil.EpochTime(head.Height(), bh.Height))
			}
		}
		tsk = ts.Parents()
		ts, err = napi.ChainGetTipSet(ctx, tsk)
		if err != nil {
			return err
		}
	}

	if tty {
		_, _ = fmt.Fprint(os.Stderr, "\r\x1b[0K")
	}

	return nil
}
