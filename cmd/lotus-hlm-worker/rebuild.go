package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
)

type RebuildTask struct {
	SectorNumber abi.SectorNumber
	ProofType    abi.RegisteredSealProof
	TicketValue  abi.SealRandomness
	SeedValue    abi.InteractiveSealRandomness

	apOut []abi.PieceInfo
	p1Out storage.PreCommit1Out
	p2Out storage.SectorCids
}

var rebuildCmd = &cli.Command{
	Name:  "rebuild",
	Usage: "rebuild the sectors",
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:  "miner-id",
			Value: 0,
		},
		&cli.Uint64Flag{
			Name:  "sector-size",
			Value: 32 * 1024 * 1024 * 1024,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context
		if !cctx.Args().Present() {
			return fmt.Errorf("must specify path for task list")
		}

		workerRepo, err := homedir.Expand(cctx.String("worker-repo"))
		if err != nil {
			return errors.As(err)
		}
		minerId := cctx.Uint64("miner-id")
		if minerId == 0 {
			return errors.New("need input miner id")
		}
		taskData, err := ioutil.ReadFile(cctx.Args().First())
		if err != nil {
			return errors.As(err, cctx.Args().First())
		}
		diskPool := NewDiskPool(abi.SectorSize(cctx.Uint64("sector-size")), ffiwrapper.WorkerCfg{}, workerRepo)
		mapstr, err := diskPool.ShowExt()
		if err != nil {
			return err
		}
		log.Infof("new diskPool instance, worker ssd -> sector map tupple is:\r\n %+v", mapstr)

		workMu := sync.Mutex{}
		sealers := map[string]*ffiwrapper.Sealer{}
		getSealer := func(minerId uint64, num abi.SectorNumber) (*ffiwrapper.Sealer, error) {
			workMu.Lock()
			defer workMu.Unlock()

			sectorName := fmt.Sprintf("s-t0%d-%d", minerId, num)
			dpState, err := diskPool.Allocate(sectorName)
			if err != nil {
				return nil, errors.As(err)
			}
			sealer, ok := sealers[dpState.MountPoint]
			if !ok {
				sealer, err = ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
					Root: dpState.MountPoint,
				})
				if err != nil {
					return nil, errors.As(err)
				}
			}
			sealers[dpState.MountPoint] = sealer
			return sealer, nil
		}

		// get the task list from file
		taskList := []RebuildTask{}
		if err := json.Unmarshal(taskData, &taskList); err != nil {
			return errors.As(err)
		}
		taskListLen := len(taskList)
		log.Infof("task len:%d", taskListLen)

		apOut := make(chan interface{}, taskListLen)
		for _, task := range taskList {
			// parallel number is taskListLen
			go func(task *RebuildTask) {
				sealer, err := getSealer(minerId, task.SectorNumber)
				if err != nil {
					apOut <- errors.As(err)
					return
				}
				sector := storage.SectorRef{
					ID: abi.SectorID{
						Miner:  abi.ActorID(minerId),
						Number: task.SectorNumber,
					},
					ProofType: task.ProofType,
				}
				// addpiece
				pieceInfo, err := sealer.PledgeSector(ctx,
					sector,
					[]abi.UnpaddedPieceSize{},
					2032,
				)
				if err != nil {
					apOut <- errors.As(err)
					return
				}
				task.apOut = pieceInfo
				apOut <- task
			}(&task)
		}

		p1Out := make(chan interface{}, taskListLen)
		for i := taskListLen; i > 0; i-- {
			task := <-apOut
			err, ok := task.(error)
			if ok {
				p1Out <- errors.As(err)
				continue
			}
			// parallel number is taskListLen
			go func(task *RebuildTask) {
				// p1
				sealer, err := getSealer(minerId, task.SectorNumber)
				if err != nil {
					p1Out <- errors.As(err)
					return
				}
				sector := storage.SectorRef{
					ID: abi.SectorID{
						Miner:  abi.ActorID(minerId),
						Number: task.SectorNumber,
					},
					ProofType: task.ProofType,
				}
				rspco, err := sealer.SealPreCommit1(ctx, sector, task.TicketValue, task.apOut)
				if err != nil {
					p1Out <- errors.As(err)
					return
				}
				task.p1Out = rspco
				p1Out <- task
			}(task.(*RebuildTask))
		}

		p2Out := make(chan interface{}, taskListLen)
		for i := taskListLen; i > 0; i-- {
			t := <-p1Out
			err, ok := t.(error)
			if ok {
				p2Out <- errors.As(err)
				continue
			}
			task := t.(*RebuildTask)

			// p2
			sealer, err := getSealer(minerId, task.SectorNumber)
			if err != nil {
				p2Out <- errors.As(err)
				continue
			}
			sector := storage.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(minerId),
					Number: task.SectorNumber,
				},
				ProofType: task.ProofType,
			}
			cids, err := sealer.SealPreCommit2(ctx, sector, task.p1Out)
			if err != nil {
				p2Out <- errors.As(err)
				continue
			}
			task.p2Out = cids
			p2Out <- task
		}

		c1Out := make(chan interface{}, taskListLen)
		for i := taskListLen; i > 0; i-- {
			t := <-p2Out
			err, ok := t.(error)
			if ok {
				c1Out <- errors.As(err)
				continue
			}
			task := t.(*RebuildTask)

			// c1
			sealer, err := getSealer(minerId, task.SectorNumber)
			if err != nil {
				c1Out <- errors.As(err)
				continue
			}
			sector := storage.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(minerId),
					Number: task.SectorNumber,
				},
				ProofType: task.ProofType,
			}
			if _, err := sealer.SealCommit1(ctx, sector, task.TicketValue, task.SeedValue, task.apOut, task.p2Out); err != nil {
				c1Out <- errors.As(err)
				continue
			}

			// ignore c2

			// do finalize
			if err := sealer.FinalizeSector(ctx, sector, nil); err != nil {
				c1Out <- errors.As(err)
				continue
			}
			c1Out <- task
		}

		// waiting the result
		for i := taskListLen; i > 0; i-- {
			t := <-c1Out
			err, ok := t.(error)
			if ok {
				log.Error(err)
				continue
			}
			task := t.(*RebuildTask)

			log.Infof("sector %+v done", task.SectorNumber)
		}

		return nil
	},
}
