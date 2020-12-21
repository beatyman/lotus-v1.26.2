package miner

import (
	"context"
	"fmt"
	"time"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	cbor "github.com/ipfs/go-ipld-cbor"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/apibstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/blockstore"
	"github.com/filecoin-project/lotus/lib/bufbstore"

	buriedmodel "github.com/filecoin-project/lotus/buried/model"

	"github.com/filecoin-project/go-address"
)

// CollectMinerInfo :
func CollectMinerInfo(cctx *cli.Context) (*buriedmodel.MinerInfo, error) {
	nodeAPI, closer, err := lcli.GetStorageMinerAPI(cctx)
	if err != nil {
		return nil, err
	}
	defer closer()

	api, acloser, err := lcli.GetFullNodeAPI(cctx)
	if err != nil {
		return nil, err
	}
	defer acloser()

	ctx := lcli.ReqContext(cctx)

	maddr, err := getActorAddress(ctx, nodeAPI, cctx.String("actor"))
	if err != nil {
		return nil, err
	}

	mact, err := api.StateGetActor(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	tbs := bufbstore.NewTieredBstore(apibstore.NewAPIBlockstore(api), blockstore.NewTemporary())
	mas, err := miner.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), mact)
	if err != nil {
		return nil, err
	}

	minerInfo := new(buriedmodel.MinerInfo)

	// log.Infof("Miner: %s\n", color.BlueString("%s", maddr))

	// Miner ID
	minerInfo.MinerID = fmt.Sprintf("%s", maddr)

	// Sector size
	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	// log.Infof("Sector Size: %s\n", types.SizeStr(types.NewInt(uint64(mi.SectorSize))))

	// Sector size
	minerInfo.SectorSize = uint64(mi.SectorSize)
	minerInfo.PeerId = mi.PeerId.String()
	minerInfo.CreateTime = time.Now().Unix()
	pow, err := api.StateMinerPower(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	// rpercI := types.BigDiv(types.BigMul(pow.MinerPower.RawBytePower, types.NewInt(1000000)), pow.TotalPower.RawBytePower)
	// qpercI := types.BigDiv(types.BigMul(pow.MinerPower.QualityAdjPower, types.NewInt(1000000)), pow.TotalPower.QualityAdjPower)

	// log.Infof("Byte Power:   %s / %s (%0.4f%%)\n",
	// 	color.BlueString(types.SizeStr(pow.MinerPower.RawBytePower)),
	// 	types.SizeStr(pow.TotalPower.RawBytePower),
	// 	float64(rpercI.Int64())/10000)

	// log.Infof("Actual Power: %s / %s (%0.4f%%)\n",
	// 	color.GreenString(types.DeciStr(pow.MinerPower.QualityAdjPower)),
	// 	types.DeciStr(pow.TotalPower.QualityAdjPower),
	// 	float64(qpercI.Int64())/10000)

	// secCounts, err := api.StateMinerSectorCount(ctx, maddr, types.EmptyTSK)
	// if err != nil {
	// 	return err
	// }

	// Raw Byte Power
	minerInfo.RawBytePower = pow.MinerPower.RawBytePower.Int64()
	// Actual Byte Total Power
	minerInfo.RawByteTotalPower = pow.TotalPower.RawBytePower.Int64()
	// Actual Byte Power
	minerInfo.ActualBytePower = pow.MinerPower.QualityAdjPower.Int64()
	// ActualByteTotalPower
	minerInfo.ActualByteTotalPower = pow.TotalPower.QualityAdjPower.Int64()

	secCounts, err := api.StateMinerSectorCount(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	proving := secCounts.Active + secCounts.Faulty
	nfaults := secCounts.Faulty

	// ActualBytePowerCommited
	committedBigInt := types.BigMul(types.NewInt(secCounts.Live), types.NewInt(uint64(mi.SectorSize)))
	minerInfo.ActualBytePowerCommited = committedBigInt.Int64()

	// log.Printf("\tCommitted: %s\n", types.SizeStr(committedBigInt))
	if nfaults == 0 {
		// ActualBytePowerProving
		provingBigInt := types.BigMul(types.NewInt(proving), types.NewInt(uint64(mi.SectorSize)))
		minerInfo.ActualBytePowerProving = provingBigInt.Int64()
		// log.Printf("\tProving: %s\n", types.SizeStr(provingBigInt))
	} else {
		// var faultyPercentage float64
		// if secCounts.Live != 0 {
		// 	faultyPercentage = float64(10000*nfaults/secCounts.Live) / 100.
		// }
		// log.Printf("\tProving: %s (%s Faulty, %.2f%%)\n",
		// 	types.SizeStr(types.BigMul(types.NewInt(proving), types.NewInt(uint64(mi.SectorSize)))),
		// 	types.SizeStr(types.BigMul(types.NewInt(nfaults), types.NewInt(uint64(mi.SectorSize)))),
		// 	faultyPercentage)
	}

	// if !pow.HasMinPower {
	// 	// fmt.Print("Below minimum power threshold, no blocks will be won")
	// } else {
	// 	expWinChance := float64(types.BigMul(qpercI, types.NewInt(build.BlocksPerEpoch)).Int64()) / 1000000
	// 	if expWinChance > 0 {
	// 		if expWinChance > 1 {
	// 			expWinChance = 1
	// 		}
	// 		winRate := time.Duration(float64(time.Second*time.Duration(build.BlockDelaySecs)) / expWinChance)
	// 		winPerDay := float64(time.Hour*24) / float64(winRate)

	// 		// fmt.Print("Expected block win rate: ")
	// 		// color.Blue("%.4f/day (every %s)", winPerDay, winRate.Truncate(time.Second))
	// 	}
	// }

	// NOTE: there's no need to unlock anything here. Funds only
	// vest on deadline boundaries, and they're unlocked by cron.
	lockedFunds, err := mas.LockedFunds()
	if err != nil {
		return nil, xerrors.Errorf("getting locked funds: %w", err)
	}
	availBalance, err := mas.AvailableBalance(mact.Balance)
	if err != nil {
		return nil, xerrors.Errorf("getting available balance: %w", err)
	}
	// log.Infof("Miner Balance: %s\n", color.YellowString("%s", types.FIL(mact.Balance)))
	// log.Infof("\tPreCommit:   %s\n", types.FIL(lockedFunds.PreCommitDeposits))
	// log.Infof("\tPledge:      %s\n", types.FIL(lockedFunds.InitialPledgeRequirement))
	// log.Infof("\tVesting:     %s\n", types.FIL(lockedFunds.VestingFunds))
	// color.Green("\tAvailable:   %s", types.FIL(availBalance))
	wb, err := api.WalletBalance(ctx, mi.Worker)
	if err != nil {
		return nil, xerrors.Errorf("getting worker balance: %w", err)
	}
	// color.Cyan("Worker Balance: %s", types.FIL(wb))

	mb, err := api.StateMarketBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting market balance: %w", err)
	}
	// log.Infof("Market (Escrow):  %s\n", types.FIL(mb.Escrow))
	// log.Infof("Market (Locked):  %s\n", types.FIL(mb.Locked))

	minerInfo.MinerBalance = mact.Balance.String()
	minerInfo.MinerBalancePrecommit = lockedFunds.PreCommitDeposits.String()
	minerInfo.MinerBalancePledge = lockedFunds.InitialPledgeRequirement.String()
	minerInfo.MinerBalanceVesting = lockedFunds.VestingFunds.String()
	minerInfo.MinerBalanceAvailable = availBalance.String()
	minerInfo.WorkerBalance = wb.String()
	minerInfo.MarketBalanceEscrow = mb.Escrow.String()
	minerInfo.MarketBalanceLocked = mb.Locked.String()

	// log.Infof("MinerInfo: %+v", minerInfo)

	return minerInfo, nil
}

func getActorAddress(ctx context.Context, nodeAPI api.StorageMiner, overrideMaddr string) (maddr address.Address, err error) {
	if overrideMaddr != "" {
		maddr, err = address.NewFromString(overrideMaddr)
		if err != nil {
			return maddr, err
		}
		return
	}

	maddr, err = nodeAPI.ActorAddress(ctx)
	if err != nil {
		return maddr, xerrors.Errorf("getting actor address: %w", err)
	}

	return maddr, nil
}
