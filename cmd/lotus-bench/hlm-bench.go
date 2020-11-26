package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	"github.com/minio/blake2b-simd"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var hlmBenchCmd = &cli.Command{
	Name:  "hlm-run",
	Usage: "Benchmark seal and winning post and window post",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "storage-dir",
			Value: "~/.lotus-bench",
			Usage: "path to the storage directory that will store sectors long term",
		},
		&cli.StringFlag{
			Name:  "sector-size",
			Value: "512MiB",
			Usage: "size of the sectors in bytes, i.e. 32GiB",
		},
		&cli.BoolFlag{
			Name:  "no-gpu",
			Usage: "disable gpu usage for the benchmark run",
		},
		&cli.IntFlag{
			Name:  "max-tasks",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "parallel-addpiece",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "parallel-precommit1",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "parallel-precommit2",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "parallel-commit1",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "parallel-commit2",
			Value: 1,
		},
	},
	Action: func(c *cli.Context) error {
		policy.AddSupportedProofTypes(abi.RegisteredSealProof_StackedDrg2KiBV1)

		return doBench(c)
	},
}

type HlmBenchTask struct {
	Type int

	TicketPreimage []byte

	ProofType abi.RegisteredSealProof
	SectorID  abi.SectorID

	// preCommit1
	Pieces []abi.PieceInfo // commit1 is need too.

	// preCommit2
	PreCommit1Out storage.PreCommit1Out

	// commit1
	Cids storage.SectorCids

	// commit2
	Commit1Out storage.Commit1Out

	// result
	Proof storage.Proof
}

func (t *HlmBenchTask) SectorName() string {
	return storage.SectorName(t.SectorID)
}

type HlmBenchResult struct {
	err       error
	startTime time.Time
	endTime   time.Time
}

var (
	apQueue  int32
	apLimit  int32
	apChan   chan *HlmBenchTask
	apResult = map[string]HlmBenchResult{}

	p1Queue  int32
	p1Limit  int32
	p1Chan   chan *HlmBenchTask
	p1Result = map[string]HlmBenchResult{}

	p2Queue  int32
	p2Limit  int32
	p2Chan   chan *HlmBenchTask
	p2Result = map[string]HlmBenchResult{}

	c1Queue  int32
	c1Limit  int32
	c1Chan   chan *HlmBenchTask
	c1Result = map[string]HlmBenchResult{}

	c2Queue  int32
	c2Limit  int32
	c2Chan   chan *HlmBenchTask
	c2Result = map[string]HlmBenchResult{}

	doneEvent chan *HlmBenchTask
)

const (
	TASK_KIND_ADDPIECE   = 0
	TASK_KIND_PRECOMMIT1 = 10
	TASK_KIND_PRECOMMIT2 = 20
	TASK_KIND_COMMIT1    = 30
	TASK_KIND_COMMIT2    = 40
)

func canParallel(kind int) bool {
	switch kind {
	case TASK_KIND_ADDPIECE:
		return atomic.LoadInt32(&apQueue) <= apLimit
	case TASK_KIND_PRECOMMIT1:
		return atomic.LoadInt32(&p1Queue) <= apLimit && atomic.LoadInt32(&apQueue) <= 0
	case TASK_KIND_PRECOMMIT2:
		return atomic.LoadInt32(&p2Queue) <= apLimit && atomic.LoadInt32(&c2Queue) <= 0
	case TASK_KIND_COMMIT1:
		return atomic.LoadInt32(&c1Queue) <= apLimit
	case TASK_KIND_COMMIT2:
		return atomic.LoadInt32(&c2Queue) <= apLimit && atomic.LoadInt32(&p2Queue) <= 0
	}
	panic("not reach here")
}

func returnTask(task *HlmBenchTask) {
	switch task.Type {
	case TASK_KIND_ADDPIECE:
		atomic.AddInt32(&apQueue, -1)
		apChan <- task
		return
	case TASK_KIND_PRECOMMIT1:
		atomic.AddInt32(&p1Queue, -1)
		p1Chan <- task
		return
	case TASK_KIND_PRECOMMIT2:
		atomic.AddInt32(&p2Queue, -1)
		p2Chan <- task
		return
	case TASK_KIND_COMMIT1:
		atomic.AddInt32(&c1Queue, -1)
		c1Chan <- task
		return
	case TASK_KIND_COMMIT2:
		atomic.AddInt32(&c2Queue, -1)
		c2Chan <- task
		return
	}
	panic(fmt.Sprintf("not reach here:%d", task.Type))
}

func doBench(c *cli.Context) error {
	if c.Bool("no-gpu") {
		err := os.Setenv("BELLMAN_NO_GPU", "1")
		if err != nil {
			return xerrors.Errorf("setting no-gpu flag: %w", err)
		}
	}
	maxTask := c.Int("max-tasks")
	apLimit = int32(c.Int("parallel-addpiece"))
	p1Limit = int32(c.Int("parallel-precommit1"))
	p2Limit = int32(c.Int("parallel-precommit2"))
	c1Limit = int32(c.Int("parallel-commit1"))
	c2Limit = int32(c.Int("parallel-commit2"))
	apChan = make(chan *HlmBenchTask, apLimit)
	p1Chan = make(chan *HlmBenchTask, p1Limit)
	p2Chan = make(chan *HlmBenchTask, p2Limit)
	c1Chan = make(chan *HlmBenchTask, c1Limit)
	c2Chan = make(chan *HlmBenchTask, c2Limit)
	doneEvent = make(chan *HlmBenchTask, maxTask)

	// build repo
	sdir, err := homedir.Expand(c.String("storage-dir"))
	if err != nil {
		return errors.As(err)
	}
	defer func() {
		if err := os.RemoveAll(sdir); err != nil {
			log.Warn("remove all: ", err)
		}
	}()
	err = os.MkdirAll(sdir, 0775) //nolint:gosec
	if err != nil {
		return xerrors.Errorf("creating sectorbuilder dir: %w", err)
	}
	sbfs := &basicfs.Provider{
		Root: sdir,
	}

	sb, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, sbfs)
	if err != nil {
		return errors.As(err)
	}

	ctx := lcli.ReqContext(c)
	// send task event
	go func() {
		for i := 0; i < maxTask; i++ {
			apChan <- &HlmBenchTask{
				Type:      TASK_KIND_ADDPIECE,
				ProofType: 0, // TODO
				SectorID: abi.SectorID{
					1000,
					abi.SectorNumber(i),
				},
				TicketPreimage: []byte(uuid.New().String()),
			}
		}
	}()

	end := maxTask
	for {
		select {
		case task := <-apChan:
			atomic.AddInt32(&apQueue, 1)
			go prepareTask(ctx, sb, task)
		case task := <-p1Chan:
			atomic.AddInt32(&p1Queue, 1)
			go prepareTask(ctx, sb, task)
		case task := <-p2Chan:
			atomic.AddInt32(&p2Queue, 1)
			go prepareTask(ctx, sb, task)
		case task := <-c1Chan:
			atomic.AddInt32(&c1Queue, 1)
			go prepareTask(ctx, sb, task)
		case task := <-c2Chan:
			atomic.AddInt32(&c2Queue, 1)
			go prepareTask(ctx, sb, task)
		case <-ctx.Done():
			// exit
			return nil
		case <-doneEvent:
			end--
			if end == 0 {
				// TODO: pring the result
				return nil
			}
		}
	}
}

func prepareTask(ctx context.Context, sb *ffiwrapper.Sealer, task *HlmBenchTask) {
	if !canParallel(task.Type) {
		log.Infof("parallel limitted, return task type:%d", task.Type)
		time.Sleep(10e9)
		returnTask(task)
		return
	}
	runTask(ctx, sb, task)
}

func runTask(ctx context.Context, sb *ffiwrapper.Sealer, task *HlmBenchTask) {
	sectorSize := abi.SectorSize(2048) // TODO: get from proof type
	sid := storage.SectorRef{
		ID:        task.SectorID,
		ProofType: spt(sectorSize),
	}
	switch task.Type {
	case TASK_KIND_ADDPIECE:
		defer atomic.AddInt32(&apQueue, -1)

		startTime := time.Now()
		r := rand.New(rand.NewSource(100 + int64(task.SectorID.Number)))
		pi, err := sb.AddPiece(ctx, sid, nil, abi.PaddedPieceSize(sectorSize).Unpadded(), r)
		if err != nil {
			panic(err) // failed
		}
		apResult[task.SectorName()] = HlmBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		}

		task.Type = TASK_KIND_PRECOMMIT1
		task.Pieces = []abi.PieceInfo{
			pi,
		}
		p1Chan <- task
		return

	case TASK_KIND_PRECOMMIT1:
		defer atomic.AddInt32(&p1Queue, -1)

		startTime := time.Now()
		trand := blake2b.Sum256(task.TicketPreimage)
		ticket := abi.SealRandomness(trand[:])
		pc1o, err := sb.SealPreCommit1(ctx, sid, ticket, task.Pieces)
		if err != nil {
			panic(err)
		}
		p1Result[task.SectorName()] = HlmBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		}

		task.Type = TASK_KIND_PRECOMMIT2
		task.PreCommit1Out = pc1o
		p2Chan <- task
		return
	case TASK_KIND_PRECOMMIT2:
		defer atomic.AddInt32(&p2Queue, -1)
		startTime := time.Now()
		cids, err := sb.SealPreCommit2(ctx, sid, task.PreCommit1Out)
		if err != nil {
			panic(err)
		}
		p2Result[task.SectorName()] = HlmBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		}

		task.Type = TASK_KIND_COMMIT1
		task.Cids = cids
		c1Chan <- task
		return
	case TASK_KIND_COMMIT1:
		defer atomic.AddInt32(&c1Queue, -1)

		startTime := time.Now()
		seed := lapi.SealSeed{
			Epoch: 101,
			Value: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 255},
		}
		trand := blake2b.Sum256(task.TicketPreimage)
		ticket := abi.SealRandomness(trand[:])
		c1o, err := sb.SealCommit1(ctx, sid, ticket, seed.Value, task.Pieces, task.Cids)
		if err != nil {
			panic(err)
		}
		c1Result[task.SectorName()] = HlmBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		}

		task.Type = TASK_KIND_COMMIT2
		task.Commit1Out = c1o
		c2Chan <- task
		return

	case TASK_KIND_COMMIT2:
		defer atomic.AddInt32(&c2Queue, -1)
		startTime := time.Now()
		proof, err := sb.SealCommit2(ctx, sid, task.Commit1Out)
		if err != nil {
			panic(err)
		}
		c2Result[task.SectorName()] = HlmBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		}

		task.Type = 200
		task.Proof = proof
		doneEvent <- task
		return
	}
	panic(fmt.Sprintf("not reach here:%d", task.Type))
}
