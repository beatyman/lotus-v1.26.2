package main

import (
	"fmt"
	"strconv"

	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/build"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"

	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
)

var sectorsCmd = &cli.Command{
	Name:  "sectors",
	Usage: "Tools for interacting with sectors",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		terminateSectorCmd,
		terminateSectorPenaltyEstimationCmd,
	},
}

var terminateSectorCmd = &cli.Command{
	Name:      "terminate",
	Usage:     "Forcefully terminate a sector (WARNING: This means losing power and pay a one-time termination penalty(including collateral) for the terminated sector)",
	ArgsUsage: "[sectorNum1 sectorNum2 ...]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "pass this flag if you know what you are doing",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 1 {
			return fmt.Errorf("at least one sector must be specified")
		}

		if !cctx.Bool("really-do-it") {
			return fmt.Errorf("this is a command for advanced users, only use it if you are sure of what you are doing")
		}

		nodeApi, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		api, acloser, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		maddr, err := api.ActorAddress(ctx)
		if err != nil {
			return err
		}

		mi, err := nodeApi.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		terminationDeclarationParams := []miner2.TerminationDeclaration{}

		for _, sn := range cctx.Args().Slice() {
			sectorNum, err := strconv.ParseUint(sn, 10, 64)
			if err != nil {
				return fmt.Errorf("could not parse sector number: %w", err)
			}

			sectorbit := bitfield.New()
			sectorbit.Set(sectorNum)

			loca, err := nodeApi.StateSectorPartition(ctx, maddr, abi.SectorNumber(sectorNum), types.EmptyTSK)
			if err != nil {
				return fmt.Errorf("get state sector partition %s", err)
			}

			para := miner2.TerminationDeclaration{
				Deadline:  loca.Deadline,
				Partition: loca.Partition,
				Sectors:   sectorbit,
			}

			terminationDeclarationParams = append(terminationDeclarationParams, para)
		}

		terminateSectorParams := &miner2.TerminateSectorsParams{
			Terminations: terminationDeclarationParams,
		}

		sp, err := actors.SerializeParams(terminateSectorParams)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		smsg, err := nodeApi.MpoolPushMessage(ctx, build.GetHlmAuth(), &types.Message{
			From:   mi.Owner,
			To:     maddr,
			Method: miner.Methods.TerminateSectors,

			Value:  big.Zero(),
			Params: sp,
		}, nil)
		if err != nil {
			return xerrors.Errorf("mpool push message: %w", err)
		}

		fmt.Println("sent termination message:", smsg.Cid())

		wait, err := nodeApi.StateWaitMsg(ctx, smsg.Cid(), uint64(cctx.Int("confidence")))
		if err != nil {
			return err
		}

		if wait.Receipt.ExitCode != 0 {
			return fmt.Errorf("terminate sectors message returned exit %d", wait.Receipt.ExitCode)
		}

		return nil
	},
}

func findPenaltyInInternalExecutions(prefix string, trace []types.ExecutionTrace) {
	for _, im := range trace {
		if im.Msg.To.String() == "f099" /*Burn actor*/ {
			fmt.Printf("Estimated termination penalty: %s attoFIL\n", im.Msg.Value)
			return
		}
		findPenaltyInInternalExecutions(prefix+"\t", im.Subcalls)
	}
}

var terminateSectorPenaltyEstimationCmd = &cli.Command{
	Name:      "termination-estimate",
	Usage:     "Estimate the termination penalty",
	ArgsUsage: "[sectorNum1 sectorNum2 ...]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 1 {
			return fmt.Errorf("at least one sector must be specified")
		}

		nodeApi, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		api, acloser, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := lcli.ReqContext(cctx)

		maddr, err := api.ActorAddress(ctx)
		if err != nil {
			return err
		}

		mi, err := nodeApi.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		terminationDeclarationParams := []miner2.TerminationDeclaration{}

		for _, sn := range cctx.Args().Slice() {
			sectorNum, err := strconv.ParseUint(sn, 10, 64)
			if err != nil {
				return fmt.Errorf("could not parse sector number: %w", err)
			}

			sectorbit := bitfield.New()
			sectorbit.Set(sectorNum)

			loca, err := nodeApi.StateSectorPartition(ctx, maddr, abi.SectorNumber(sectorNum), types.EmptyTSK)
			if err != nil {
				return fmt.Errorf("get state sector partition %s", err)
			}

			para := miner2.TerminationDeclaration{
				Deadline:  loca.Deadline,
				Partition: loca.Partition,
				Sectors:   sectorbit,
			}

			terminationDeclarationParams = append(terminationDeclarationParams, para)
		}

		terminateSectorParams := &miner2.TerminateSectorsParams{
			Terminations: terminationDeclarationParams,
		}

		sp, err := actors.SerializeParams(terminateSectorParams)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		msg := &types.Message{
			From:   mi.Owner,
			To:     maddr,
			Method: miner.Methods.TerminateSectors,

			Value:  big.Zero(),
			Params: sp,
		}

		//TODO: 4667 add an option to give a more precise estimation with pending termination penalty excluded

		invocResult, err := nodeApi.StateCall(ctx, msg, types.TipSetKey{})
		if err != nil {
			return xerrors.Errorf("fail to state call: %w", err)
		}

		findPenaltyInInternalExecutions("\t", invocResult.ExecutionTrace.Subcalls)
		return nil
	},
}
