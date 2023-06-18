package main

import (
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"io/ioutil"
	"path/filepath"
	"sync"

	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
)

type RebuildTask struct {
	SectorNumber abi.SectorNumber
	ProofType    abi.RegisteredSealProof
	TicketValue  abi.SealRandomness
	SeedValue    abi.InteractiveSealRandomness

	apOut []abi.PieceInfo
	p1Out storiface.PreCommit1Out
	p2Out storiface.SectorCids
}

//// json format
//{
//  "AuthUri":"127.0.0.1:1330",
//	"AuthMd5":"", // md5
//	"TransfUri":"127.0.0.1:1331",
//  "Parallel": 12,
//	"Tasks":[{
//		// RebuildTask
//	}]
//}
type RebuildTasks struct {
	AuthUri   string
	AuthMd5   string
	TransfUri string
	Parallel  int
	Tasks     []RebuildTask
}

var rebuildCmd = &cli.Command{
	Name:  "rebuild",
	Usage: "rebuild the sectors",
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:  "miner-id",
			Value: 0,
		},
		&cli.Int64Flag{
			Name:  "sector-size",
			Value: 2048,
		},
		&cli.StringFlag{
			Name:  "sector-head",
			Value: "s-f",
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
		ssize := abi.SectorSize(cctx.Int64("sector-size"))
		storiface.SectorHead = cctx.String("sector-head")
		// TODO: sector size
		diskPool := NewDiskPool(ssize, ffiwrapper.WorkerCfg{}, workerRepo)
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
			sectorName := storiface.SectorName(abi.SectorID{Miner: abi.ActorID(minerId), Number: num})
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

		// get task
		taskData, err := ioutil.ReadFile(cctx.Args().First())
		if err != nil {
			return errors.As(err, cctx.Args().First())
		}
		// get the task list from file
		taskList := RebuildTasks{}
		if err := json.Unmarshal(taskData, &taskList); err != nil {
			return errors.As(err)
		}
		taskListLen := len(taskList.Tasks)
		log.Infof("task len:%d", taskListLen)

		totalTask := make(chan *RebuildTask, taskListLen)
		for i := 0; i < taskListLen; i++ {
			totalTask <- &(taskList.Tasks[i])
		}
		parallel := taskList.Parallel
		if parallel > taskListLen {
			parallel = taskListLen
		}

		producer := make(chan *RebuildTask, parallel)
		apOut := make(chan interface{}, parallel)
		p1Out := make(chan interface{}, parallel)
		p2Out := make(chan interface{}, 1)
		result := make(chan interface{}, 2)

		// do addpiece
		for i := parallel; i > 0; i-- {
			producer <- (<-totalTask) // for init

			go func() {
				for {
					task := <-producer
					sealer, err := getSealer(minerId, task.SectorNumber)
					if err != nil {
						apOut <- errors.As(err)
						continue
					}
					sector := storiface.SectorRef{
						ID: abi.SectorID{
							Miner:  abi.ActorID(minerId),
							Number: task.SectorNumber,
						},
						ProofType: task.ProofType,
					}
					ssize, err := task.ProofType.SectorSize()
					if err != nil {
						apOut <- errors.As(err)
						continue
					}
					// addpiece
					pieceInfo, err := sealer.PledgeSector(ctx,
						sector,
						[]abi.UnpaddedPieceSize{},
						abi.PaddedPieceSize(ssize).Unpadded(),
					)
					if err != nil {
						apOut <- errors.As(err)
						continue
					}
					task.apOut = pieceInfo
					apOut <- task
				}
			}()
		}

		// do precommit1
		for i := parallel; i > 0; i-- {
			// parallel number is taskListLen
			go func() {
				for {
					t := <-apOut
					err, ok := t.(error)
					if ok {
						p1Out <- errors.As(err)
						continue
					}
					task := t.(*RebuildTask)

					// p1
					sealer, err := getSealer(minerId, task.SectorNumber)
					if err != nil {
						p1Out <- errors.As(err)
						continue
					}
					sector := storiface.SectorRef{
						ID: abi.SectorID{
							Miner:  abi.ActorID(minerId),
							Number: task.SectorNumber,
						},
						ProofType: task.ProofType,
					}
					rspco, err := sealer.SealPreCommit1(ctx, sector, task.TicketValue, task.apOut)
					if err != nil {
						p1Out <- errors.As(err)
						continue
					}
					task.p1Out = rspco
					p1Out <- task
				}
			}()
		}

		// do precommit2
		go func() {
			for {
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
				sector := storiface.SectorRef{
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
		}()

		// do commit1 and finalize
		go func() {
			for {
				t := <-p2Out
				err, ok := t.(error)
				if ok {
					result <- errors.As(err)
					continue
				}
				task := t.(*RebuildTask)

				// TODO: ignore c1 ?
				sealer, err := getSealer(minerId, task.SectorNumber)
				if err != nil {
					result <- errors.As(err)
					continue
				}
				sector := storiface.SectorRef{
					ID: abi.SectorID{
						Miner:  abi.ActorID(minerId),
						Number: task.SectorNumber,
					},
					ProofType: task.ProofType,
				}
				//if _, err := sealer.SealCommit1(ctx, sector, task.TicketValue, task.SeedValue, task.apOut, task.p2Out); err != nil {
				//	result <- errors.As(err)
				//	continue
				//}

				// ignore c2

				// do finalize
				if err := sealer.FinalizeSector(ctx, sector); err != nil {
					result <- errors.As(err)
					continue
				}

				sid := storiface.SectorName(sector.ID)
				mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
				// send the sealed
				sealedFromPath := sealer.SectorPath("sealed", sid)
				sealedToPath := filepath.Join(mountDir, "sealed")
				w := worker{}
				if err := w.upload(ctx, sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
					log.Error(err)
				}
				// send the cache
				cacheFromPath := sealer.SectorPath("cache", sid)
				cacheToPath := filepath.Join(mountDir, "cache", sid)
				if err := w.upload(ctx, cacheFromPath, cacheToPath); err != nil {
					log.Error(err)
				}
				result <- task
			}
		}()

		// waiting the result
		for i := 0; i < taskListLen; i++ {
			t := <-result

			producer <- (<-totalTask) // add the next addpiece

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
