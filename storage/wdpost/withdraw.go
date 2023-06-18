package wdpost

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/filecoin-project/specs-actors/v8/actors/builtin"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/config"
	sectorstorage "github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/gwaylib/errors"

	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
)

//.lotusstorage/withdraw.toml
//# auto withdraw configuration
//IntervalEpoch=6
//SendToAddr=""
//WithdrawAmount="100"
//KeepOwnerAmount="5"
type WithdrawConfig struct {
	IntervalEpoch   int64
	SendToAddr      string
	WithdrawAmount  string // FIL
	KeepOwnerAmount string // FIL
}

func (s *WindowPoStScheduler) StateWaitMsg(ctx context.Context, sm *types.SignedMessage) (*api.MsgLookup, error) {
	type WaitResult struct {
		lookup *api.MsgLookup
		err    error
	}
	result := make(chan *WaitResult, 1)
	defer close(result)

	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return nil, errors.As(err)
	}

	cancelCtx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	go func() {
		lp, err := s.api.StateWaitMsg(cancelCtx, sm.Cid(), build.MessageConfidence, head.Height()+30, true)
		result <- &WaitResult{lp, err}
	}()
	select {
	case <-time.After(10 * time.Minute):
		cancelFn()
		smsg, err := s.fixMpool(ctx, sm)
		if err != nil {
			return nil, errors.As(err)
		}

		return s.api.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, head.Height()+60, true)
	case res := <-result:
		return res.lookup, res.err
	}
}

// copy from cli/mpool.go#hlm-fix
func (s *WindowPoStScheduler) fixMpool(ctx context.Context, sm *types.SignedMessage) (*types.SignedMessage, error) {
	baseFee, err := s.api.ChainComputeBaseFee(ctx, types.EmptyTSK)
	if err != nil {
		return nil, errors.As(err)
	}
	ratePremium := uint64(12500)
	rateFeeCap := uint64(12500)
	rateLimit := int64(12500)

	// fix with replace
	newMsg := sm.Message
	retm, err := s.api.GasEstimateMessageGas(ctx, &newMsg, &api.MessageSendSpec{}, types.EmptyTSK)
	if err != nil {
		return nil, errors.As(err)
	}
	// newMsg.GasFeeCap = retm.GasFeeCap
	newMsg.GasFeeCap = types.BigAdd(
		types.BigDiv(types.BigMul(baseFee, types.NewInt(rateFeeCap)), types.NewInt(10000)),
		types.NewInt(1),
	)

	// Kubuxu said: The formula is 1.25*oldPremium + 1attoFIL
	newMsg.GasPremium = types.BigAdd(
		types.BigDiv(types.BigMul(newMsg.GasPremium, types.NewInt(ratePremium)), types.NewInt(10000)),
		types.NewInt(1),
	)
	newMsg.GasPremium = big.Max(retm.GasPremium, newMsg.GasPremium)

	// gas-limit
	newMsg.GasLimit = newMsg.GasLimit*rateLimit/10000 + 1
	smsg, err := s.api.WalletSignMessage(ctx, newMsg.From, &newMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}
	cid, err := s.api.MpoolPush(ctx, smsg)
	if err != nil {
		return nil, fmt.Errorf("failed to push new message to mempool: %w", err)
	}
	log.Warnf("BaseFee:%d, newCid:%s newMsg:%s oldMsg:%s", baseFee, cid, newMsg.String(), sm.Message.String())
	return smsg, nil
}

func (s *WindowPoStScheduler) autoWithdraw(ts *types.TipSet) {
	s.autoWithdrawLk.Lock()
	defer s.autoWithdrawLk.Unlock()
	if s.autoWithdrawRunning {
		return
	}

	// read config
	sm, ok := s.prover.(*sectorstorage.Manager)
	if !ok {
		log.Info("prover not *sectorstorage.Manager")
		return
	}
	sb, ok := sm.Prover.(*ffiwrapper.Sealer)
	if !ok {
		log.Info("prover not *ffiwrapper.Sealer")
		return
	}
	repo := sb.RepoPath()
	if len(repo) == 0 {
		log.Info("auto withdraw repo not found")
		return
	}
	cfgI, err := config.FromFile(filepath.Join(repo, "withdraw.toml"), config.SetDefault(func() (interface{}, error) { return &WithdrawConfig{}, nil }))
	if err != nil {
		log.Warn(errors.As(err))
		return
	}
	cfg, ok := cfgI.(*WithdrawConfig)
	if !ok {
		log.Info("cfgI is not ok")
		return
	}
	if cfg.IntervalEpoch <= 0 {
		return
	}
	if len(cfg.SendToAddr) == 0 {
		return
	}
	if int64(ts.Height())-s.autoWithdrawLastEpoch < cfg.IntervalEpoch {
		return
	}
	s.autoWithdrawLastEpoch = int64(ts.Height())

	go func() {
		if err := s.doWithdraw(cfg); err != nil {
			log.Error(err)
		}
	}()
}

func (s *WindowPoStScheduler) doWithdraw(cfg *WithdrawConfig) error {
	s.autoWithdrawLk.Lock()
	s.autoWithdrawRunning = true
	s.autoWithdrawLk.Unlock()
	defer func() {
		s.autoWithdrawLk.Lock()
		s.autoWithdrawRunning = false
		s.autoWithdrawLk.Unlock()
	}()
	api := s.api
	ctx := context.TODO()
	maddr := s.actor
	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return errors.As(err, *cfg)
	}
	toAddr, err := address.NewFromString(cfg.SendToAddr)

	f, err := types.ParseFIL(cfg.WithdrawAmount)
	if err != nil {
		return errors.As(err, *cfg)
	}
	amount := abi.TokenAmount(f)

	f, err = types.ParseFIL(cfg.KeepOwnerAmount)
	if err != nil {
		return errors.As(err, *cfg)
	}
	keepAmount := abi.TokenAmount(f)

	// send the history withdraw
	if err := s.withdrawSend(ctx, mi.Owner, toAddr, amount, keepAmount); err != nil {
		return errors.As(err, *cfg)
	}

	// check available balance
	available, err := api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return errors.As(err, *cfg)
	}

	if amount.GreaterThan(available) {
		log.Info("available not enough")
		// no balance for withdraw
		return nil
	}

	// withdraw
	params, err := actors.SerializeParams(&miner2.WithdrawBalanceParams{
		AmountRequested: amount, // Default to attempting to withdraw all the extra funds in the miner actor
	})
	if err != nil {
		return errors.As(err, *cfg)
	}

	msg := &types.Message{
		To:    maddr,
		From:  mi.Owner,
		Value: types.NewInt(0),

		Method: builtin.MethodsMiner.WithdrawBalance,
		Params: params,
	}
	sm, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return errors.As(err, *cfg)
	}
	if _, err := s.StateWaitMsg(ctx, sm); err != nil {
		return errors.As(err)
	}

	log.Infof("Auto withdraw miner:%s, available:%s, amount:%s, cid:%s", maddr, available, amount, sm.Cid())

	// Trying send the withdraw result after withdraw.
	if err := s.withdrawSend(ctx, mi.Owner, toAddr, amount, keepAmount); err != nil {
		return errors.As(err, *cfg)
	}
	return nil
}

func (s *WindowPoStScheduler) withdrawSend(ctx context.Context, fromAddr, toAddr address.Address, amount, keepOwner abi.TokenAmount) error {
	available, err := s.api.WalletBalance(ctx, fromAddr)
	if err != nil {
		return errors.As(err)
	}
	if amount.GreaterThan(types.BigSub(available, keepOwner)) {
		// no balance for withdraw
		log.Info("owner balance not enough")
		return nil
	}

	msg := &types.Message{
		From:   fromAddr,
		To:     toAddr,
		Value:  amount,
		Params: []byte{},
	}
	sm, err := s.api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return errors.As(err)
	}
	if _, err := s.StateWaitMsg(ctx, sm); err != nil {
		return errors.As(err)
	}

	log.Infof(
		"Auto withdraw sent, from:%s, to:%s, balance:%s, sent:%s, cid:%s",
		fromAddr, toAddr, available, amount, sm.Cid(),
	)
	return nil
}
