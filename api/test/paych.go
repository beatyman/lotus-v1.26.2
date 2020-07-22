package test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/events/state"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/abi/big"
	initactor "github.com/filecoin-project/specs-actors/actors/builtin/init"
	"github.com/filecoin-project/specs-actors/actors/builtin/paych"
)

func TestPaymentChannels(t *testing.T, b APIBuilder, blocktime time.Duration) {
	_ = os.Setenv("BELLMAN_NO_GPU", "1")

	ctx := context.Background()
	n, sn := b(t, 2, oneMiner)

	paymentCreator := n[0]
	paymentReceiver := n[1]
	miner := sn[0]

	// get everyone connected
	addrs, err := paymentCreator.NetAddrsListen(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := paymentReceiver.NetConnect(ctx, addrs); err != nil {
		t.Fatal(err)
	}

	if err := miner.NetConnect(ctx, addrs); err != nil {
		t.Fatal(err)
	}

	// start mining blocks
	bm := newBlockMiner(ctx, t, miner, blocktime)
	bm.mineBlocks()

	// send some funds to register the receiver
	receiverAddr, err := paymentReceiver.WalletNew(ctx, wallet.ActSigType("secp256k1"))
	if err != nil {
		t.Fatal(err)
	}

	sendFunds(ctx, t, paymentCreator, receiverAddr, abi.NewTokenAmount(1000))

	// setup the payment channel
	createrAddr, err := paymentCreator.WalletDefaultAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	initBalance, err := paymentCreator.WalletBalance(ctx, createrAddr)
	if err != nil {
		t.Fatal(err)
	}

	channelInfo, err := paymentCreator.PaychGet(ctx, createrAddr, receiverAddr, abi.NewTokenAmount(100000))
	if err != nil {
		t.Fatal(err)
	}
	res, err := paymentCreator.StateWaitMsg(ctx, channelInfo.ChannelMessage, 1)
	if res.Receipt.ExitCode != 0 {
		t.Fatal("did not successfully create payment channel")
	}
	var params initactor.ExecReturn
	err = params.UnmarshalCBOR(bytes.NewReader(res.Receipt.Return))
	if err != nil {
		t.Fatal(err)
	}
	channel := params.RobustAddress
	// allocate three lanes
	var lanes []uint64
	for i := 0; i < 3; i++ {
		lane, err := paymentCreator.PaychAllocateLane(ctx, channel)
		if err != nil {
			t.Fatal(err)
		}
		lanes = append(lanes, lane)
	}

	// make two vouchers each for each lane, then save on the other side
	for _, lane := range lanes {
		vouch1, err := paymentCreator.PaychVoucherCreate(ctx, channel, abi.NewTokenAmount(1000), lane)
		if err != nil {
			t.Fatal(err)
		}
		vouch2, err := paymentCreator.PaychVoucherCreate(ctx, channel, abi.NewTokenAmount(2000), lane)
		if err != nil {
			t.Fatal(err)
		}
		delta1, err := paymentReceiver.PaychVoucherAdd(ctx, channel, vouch1, nil, abi.NewTokenAmount(1000))
		if err != nil {
			t.Fatal(err)
		}
		if !delta1.Equals(abi.NewTokenAmount(1000)) {
			t.Fatal("voucher didn't have the right amount")
		}
		delta2, err := paymentReceiver.PaychVoucherAdd(ctx, channel, vouch2, nil, abi.NewTokenAmount(1000))
		if err != nil {
			t.Fatal(err)
		}
		if !delta2.Equals(abi.NewTokenAmount(1000)) {
			t.Fatal("voucher didn't have the right amount")
		}
	}

	// settle the payment channel
	settleMsgCid, err := paymentCreator.PaychSettle(ctx, channel)
	if err != nil {
		t.Fatal(err)
	}
	res, err = paymentCreator.StateWaitMsg(ctx, settleMsgCid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Receipt.ExitCode != 0 {
		t.Fatal("Unable to settle payment channel")
	}

	// wait for the receiver to submit their vouchers
	ev := events.NewEvents(ctx, paymentCreator)
	preds := state.NewStatePredicates(paymentCreator)
	finished := make(chan struct{})
	err = ev.StateChanged(func(ts *types.TipSet) (done bool, more bool, err error) {
		act, err := paymentCreator.StateReadState(ctx, channel, ts.Key())
		if err != nil {
			return false, false, err
		}
		state := act.State.(paych.State)
		if state.ToSend.GreaterThanEqual(abi.NewTokenAmount(6000)) {
			return true, false, nil
		}
		return false, true, nil
	}, func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		toSendChange := states.(*state.PayChToSendChange)
		if toSendChange.NewToSend.GreaterThanEqual(abi.NewTokenAmount(6000)) {
			close(finished)
			return false, nil
		}
		return true, nil
	}, func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}, int(build.MessageConfidence)+1, build.SealRandomnessLookbackLimit, func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		return preds.OnPaymentChannelActorChanged(channel, preds.OnToSendAmountChanges())(ctx, oldTs.Key(), newTs.Key())
	})

	<-finished

	// collect funds (from receiver, though either party can do it)
	collectMsg, err := paymentReceiver.PaychCollect(ctx, channel)
	if err != nil {
		t.Fatal(err)
	}
	res, err = paymentReceiver.StateWaitMsg(ctx, collectMsg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Receipt.ExitCode != 0 {
		t.Fatal("unable to collect on payment channel")
	}

	// Finally, check the balance for the receiver and creator
	currentCreatorBalance, err := paymentCreator.WalletBalance(ctx, createrAddr)
	if err != nil {
		t.Fatal(err)
	}
	if !big.Sub(initBalance, currentCreatorBalance).Equals(abi.NewTokenAmount(7000)) {
		t.Fatal("did not send correct funds from creator")
	}
	currentReceiverBalance, err := paymentReceiver.WalletBalance(ctx, receiverAddr)
	if err != nil {
		t.Fatal(err)
	}
	if !currentReceiverBalance.Equals(abi.NewTokenAmount(7000)) {
		t.Fatal("did not receive correct funds on receiver")
	}

	// shut down mining
	bm.stop()
}

type blockMiner struct {
	ctx       context.Context
	t         *testing.T
	miner     TestStorageNode
	blocktime time.Duration
	mine      int64
	done      chan struct{}
}

func newBlockMiner(ctx context.Context, t *testing.T, miner TestStorageNode, blocktime time.Duration) *blockMiner {
	return &blockMiner{
		ctx:       ctx,
		t:         t,
		miner:     miner,
		blocktime: blocktime,
		mine:      int64(1),
		done:      make(chan struct{}),
	}
}

func (bm *blockMiner) mineBlocks() {
	time.Sleep(time.Second)
	go func() {
		defer close(bm.done)
		for atomic.LoadInt64(&bm.mine) == 1 {
			time.Sleep(bm.blocktime)
			if err := bm.miner.MineOne(bm.ctx, func(bool, error) {}); err != nil {
				bm.t.Error(err)
			}
		}
	}()
}

func (bm *blockMiner) stop() {
	atomic.AddInt64(&bm.mine, -1)
	fmt.Println("shutting down mining")
	<-bm.done
}

func sendFunds(ctx context.Context, t *testing.T, sender TestNode, addr address.Address, amount abi.TokenAmount) {

	senderAddr, err := sender.WalletDefaultAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	msg := &types.Message{
		From:     senderAddr,
		To:       addr,
		Value:    amount,
		GasLimit: 100_000_000,
		GasPrice: abi.NewTokenAmount(1000),
	}

	sm, err := sender.MpoolPushMessage(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sender.StateWaitMsg(ctx, sm.Cid(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Receipt.ExitCode != 0 {
		t.Fatal("did not successfully send money")
	}
}
