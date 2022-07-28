package main

import (
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
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
		// TODO: sector size
		diskPool := NewDiskPool(abi.SectorSize(2048), ffiwrapper.WorkerCfg{}, workerRepo)
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
				if err := sealer.FinalizeSector(ctx, sector, nil); err != nil {
					result <- errors.As(err)
					continue
				}

				// TODO: transfer the data
				if len(taskList.TransfUri) > 0 {
					for {
						time.Sleep(1e9)

						sid := storiface.SectorName(sector.ID)
						auth := hlmclient.NewAuthClient(taskList.AuthUri, taskList.AuthMd5)
						token, err := auth.NewFileToken(ctx, sid)
						if err != nil {
							log.Error(errors.As(err, sid))
							continue
						}
						defer auth.DeleteFileToken(ctx, sid)

						fc := hlmclient.NewHttpClient(taskList.TransfUri, sid, string(token))

						// send the cache
						log.Infof("upload cache of %s", sid)
						cacheFromPath := sealer.SectorPath("cache", sid)
						if err := fc.Upload(ctx, cacheFromPath, filepath.Join("cache", sid)); err != nil {
							log.Error(errors.As(err))
							continue
						}

						// send the sealed
						log.Infof("upload sealed of %s", sid)
						sealedFromPath := sealer.SectorPath("sealed", sid)
						if err := fc.Upload(ctx, sealedFromPath, filepath.Join("sealed", sid)); err != nil {
							log.Error(errors.As(err))
							continue
						}

						// delete
						repo := sealer.RepoPath()
						log.Infof("Remove sector:%s,%s", repo, sid)
						if err := os.Remove(filepath.Join(repo, "sealed", sid)); err != nil {
							log.Error(errors.As(err, sid))
						}
						if err := os.RemoveAll(filepath.Join(repo, "cache", sid)); err != nil {
							log.Error(errors.As(err, sid))
						}
						if err := os.Remove(filepath.Join(repo, "unsealed", sid)); err != nil {
							log.Error(errors.As(err, sid))
						}
						if err := diskPool.Delete(sid); err != nil {
							log.Error(errors.As(err))
						}
						break
					}
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
