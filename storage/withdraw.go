package storage

import (
	"context"
	"path/filepath"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules/auth"
	"github.com/gwaylib/errors"

	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
)

type WithdrawConfig struct {
	IntervalEpoch   int64
	SendToAddr      string
	WithdrawAmount  string // FIL
	KeepOwnerAmount string // FIL
}

func (s *WindowPoStScheduler) autoWithdraw(ts *types.TipSet) {
	s.autoWithdrawLk.Lock()
	defer s.autoWithdrawLk.Unlock()
	if s.autoWithdrawRunning {
		return
	}

	// read config
	sb, ok := s.prover.(*ffiwrapper.Sealer)
	if !ok {
		return
	}
	repo := sb.RepoPath()
	if len(repo) == 0 {
		return
	}

	cfgI, err := config.FromFile(filepath.Join(repo, "withdraw.toml"), &WithdrawConfig{})
	if err != nil {
		// TODO: log?
		return
	}
	cfg, ok := cfgI.(*WithdrawConfig)
	if !ok {
		return
	}
	if cfg.IntervalEpoch <= 0 {
		return
	}
	if int64(ts.Height())-s.autoWithdrawLastEpoch < cfg.IntervalEpoch {
		return
	}

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
		return err
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
		return err
	}

	if amount.GreaterThan(available) {
		// no balance for withdraw
		return nil
	}

	// withdraw
	params, err := actors.SerializeParams(&miner2.WithdrawBalanceParams{
		AmountRequested: amount, // Default to attempting to withdraw all the extra funds in the miner actor
	})
	if err != nil {
		return err
	}

	msg := &types.Message{
		To:    maddr,
		From:  mi.Owner,
		Value: types.NewInt(0),

		Method: miner.Methods.WithdrawBalance,
		Params: params,
	}
	sm, err := api.MpoolPushMessage(ctx, auth.GetHlmAuth(), msg, nil)
	if err != nil {
		return errors.As(err)
	}
	log.Infof("auto withdraw miner:%s, available:%s, amount:%s, cid:%s", maddr, available, amount, sm.Cid())
	return nil
}

func (s *WindowPoStScheduler) withdrawSend(ctx context.Context, fromAddr, toAddr address.Address, amount, keepOwner abi.TokenAmount) error {
	available, err := s.api.WalletBalance(ctx, fromAddr)
	if err != nil {
		return errors.As(err)
	}
	if amount.GreaterThan(types.BigSub(available, keepOwner)) {
		// no balance for withdraw
		return nil
	}

	msg := &types.Message{
		From:   fromAddr,
		To:     toAddr,
		Value:  amount,
		Params: []byte{},
	}
	sm, err := s.api.MpoolPushMessage(ctx, auth.GetHlmAuth(), msg, nil)
	if err != nil {
		return err
	}

	log.Infof(
		"auto withdraw send, from:%s, to:%s, balance:%s, send:%s, cid:%s",
		fromAddr, toAddr, available, amount, sm.Cid(),
	)
	return nil
}
