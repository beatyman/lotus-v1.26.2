package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/go-units"
	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/policy"
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

var pBenchCmd = &cli.Command{
	Name:  "p-run",
	Usage: "Benchmark for parallel seal",
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
		&cli.BoolFlag{
			Name:  "taskset",
			Usage: "using golang cpu affinity, need run with binary program",
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

type ParallelBenchTask struct {
	Type int

	TicketPreimage []byte

	SectorSize abi.SectorSize
	SectorID   abi.SectorID

	TaskSet bool
	Repo    string

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

func (t *ParallelBenchTask) SectorName() string {
	return storage.SectorName(t.SectorID)
}

type ParallelBenchResult struct {
	startTime time.Time
	endTime   time.Time
}

func (p *ParallelBenchResult) String() string {
	return fmt.Sprintf("used:%s,start:%s,end:%s", p.endTime.Sub(p.startTime), p.startTime.Format(time.RFC3339Nano), p.endTime.Format(time.RFC3339Nano))
}

var (
	parallelLock = sync.Mutex{}

	apParallel int32
	apLimit    int32
	apChan     chan *ParallelBenchTask
	apResult   = sync.Map{}

	p1Parallel int32
	p1Limit    int32
	p1Chan     chan *ParallelBenchTask
	p1Result   = sync.Map{}

	p2Parallel int32
	p2Limit    int32
	p2Chan     chan *ParallelBenchTask
	p2Result   = sync.Map{}

	c1Parallel int32
	c1Limit    int32
	c1Chan     chan *ParallelBenchTask
	c1Result   = sync.Map{}

	c2Parallel int32
	c2Limit    int32
	c2Chan     chan *ParallelBenchTask
	c2Result   = sync.Map{}

	doneEvent chan *ParallelBenchTask
)

func Statistics(result sync.Map) (sum, min, max time.Duration) {
	min = time.Duration(math.MaxInt64)
	result.Range(func(k, v interface{}) bool {
		sectorName := k.(string)
		res := v.(ParallelBenchResult)
		used := res.endTime.Sub(res.startTime)
		if min > used {
			min = used
		}
		if max < used {
			max = used
		}
		sum += used
		fmt.Printf("%s	%s\n", sectorName, res.String())
		return true
	})
	return
}

const (
	TASK_KIND_ADDPIECE   = 0
	TASK_KIND_PRECOMMIT1 = 10
	TASK_KIND_PRECOMMIT2 = 20
	TASK_KIND_COMMIT1    = 30
	TASK_KIND_COMMIT2    = 40
)

func canParallel(kind int) bool {
	apRunning := atomic.LoadInt32(&apParallel) > 0
	p1Running := atomic.LoadInt32(&p1Parallel) > 0
	p2Running := atomic.LoadInt32(&p2Parallel) > 0
	c2Running := atomic.LoadInt32(&c2Parallel) > 0
	switch kind {
	case TASK_KIND_ADDPIECE:
		return atomic.LoadInt32(&apParallel) < apLimit
	case TASK_KIND_PRECOMMIT1:
		return atomic.LoadInt32(&p1Parallel) < p1Limit && !apRunning && !p2Running && !c2Running
	case TASK_KIND_PRECOMMIT2:
		return atomic.LoadInt32(&p2Parallel) < p2Limit && !apRunning && !p1Running && !c2Running
	case TASK_KIND_COMMIT1:
		return atomic.LoadInt32(&c1Parallel) < c1Limit
	case TASK_KIND_COMMIT2:
		return atomic.LoadInt32(&c2Parallel) < c2Limit && !apRunning && !p1Running && !p2Running
	}
	panic("not reach here")
}

func offsetParallel(task *ParallelBenchTask, offset int32) {
	switch task.Type {
	case TASK_KIND_ADDPIECE:
		atomic.AddInt32(&apParallel, offset)
		return
	case TASK_KIND_PRECOMMIT1:
		atomic.AddInt32(&p1Parallel, offset)
		return
	case TASK_KIND_PRECOMMIT2:
		atomic.AddInt32(&p2Parallel, offset)
		return
	case TASK_KIND_COMMIT1:
		atomic.AddInt32(&c1Parallel, offset)
		return
	case TASK_KIND_COMMIT2:
		atomic.AddInt32(&c2Parallel, offset)
		return
	}
	panic(fmt.Sprintf("not reach here:%d", task.Type))

}

func returnTask(task *ParallelBenchTask) {
	time.Sleep(10e9)
	switch task.Type {
	case TASK_KIND_ADDPIECE:
		apChan <- task
		return
	case TASK_KIND_PRECOMMIT1:
		p1Chan <- task
		return
	case TASK_KIND_PRECOMMIT2:
		p2Chan <- task
		return
	case TASK_KIND_COMMIT1:
		c1Chan <- task
		return
	case TASK_KIND_COMMIT2:
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
	// sector size
	sectorSizeInt, err := units.RAMInBytes(c.String("sector-size"))
	if err != nil {
		return err
	}
	sectorSize := abi.SectorSize(sectorSizeInt)
	taskset := c.Bool("taskset")
	maxTask := c.Int("max-tasks")
	apLimit = int32(c.Int("parallel-addpiece"))
	p1Limit = int32(c.Int("parallel-precommit1"))
	p2Limit = int32(c.Int("parallel-precommit2"))
	c1Limit = int32(c.Int("parallel-commit1"))
	c2Limit = int32(c.Int("parallel-commit2"))
	apChan = make(chan *ParallelBenchTask, apLimit)
	p1Chan = make(chan *ParallelBenchTask, p1Limit)
	p2Chan = make(chan *ParallelBenchTask, p2Limit)
	c1Chan = make(chan *ParallelBenchTask, c1Limit)
	c2Chan = make(chan *ParallelBenchTask, c2Limit)
	doneEvent = make(chan *ParallelBenchTask, maxTask)

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

	// event producer
	go func() {
		for i := 0; i < maxTask; i++ {
			apChan <- &ParallelBenchTask{
				Type:       TASK_KIND_ADDPIECE,
				SectorSize: sectorSize,
				SectorID: abi.SectorID{
					1000,
					abi.SectorNumber(i),
				},

				TaskSet: taskset,
				Repo:    sdir,

				TicketPreimage: []byte(uuid.New().String()),
			}
		}
	}()

	fmt.Println("[ctrl+c to exit]")
	exit := make(chan os.Signal, 2)
	signal.Notify(exit, os.Interrupt, os.Kill)
	ctx, cancel := context.WithCancel(context.TODO())

	end := maxTask
consumer:
	for {
		select {
		case task := <-apChan:
			prepareTask(ctx, sb, task)
		case task := <-p1Chan:
			prepareTask(ctx, sb, task)
		case task := <-p2Chan:
			prepareTask(ctx, sb, task)
		case task := <-c1Chan:
			prepareTask(ctx, sb, task)
		case task := <-c2Chan:
			prepareTask(ctx, sb, task)

		case task := <-doneEvent:
			fmt.Printf("done event:%s_%d\n", task.SectorName(), task.Type)
			end--
			if end > 0 {
				continue consumer
			}
			break consumer

		case <-exit:
			cancel()
		case <-ctx.Done():
			// exit
			fmt.Println("user canceled")
			break consumer
		}
	}

	// output the result
	fmt.Println("addpiece detail:")
	fmt.Println("=================")
	apSum, apMin, apMax := Statistics(apResult)
	fmt.Println()

	fmt.Println("precommit1 detail:")
	fmt.Println("=================")
	p1Sum, p1Min, p1Max := Statistics(p1Result)
	fmt.Println()

	fmt.Println("precommit2 detail:")
	fmt.Println("=================")
	p2Sum, p2Min, p2Max := Statistics(p2Result)
	fmt.Println()

	fmt.Println("commit1 detail:")
	fmt.Println("=================")
	c1Sum, c1Min, c1Max := Statistics(c1Result)
	fmt.Println()

	fmt.Println("commit2 detail:")
	fmt.Println("=================")
	c2Sum, c2Min, c2Max := Statistics(c2Result)
	fmt.Println()

	fmt.Printf(
		"total sectors:%d, parallel-addpiece:%d, parallel-precommit1:%d, parallel-precommit2:%d, parallel-commit1:%d,parallel-commit2:%d\n",
		maxTask, apLimit, p1Limit, p2Limit, c1Limit, c2Limit,
	)
	fmt.Println("=================")
	if apLimit <= 0 {
		return nil
	}
	fmt.Printf("addpiece    avg:%s, min:%s, max:%s\n", apSum/time.Duration(apLimit), apMin, apMax)
	if p1Limit <= 0 {
		return nil
	}
	fmt.Printf("precommit1 avg:%s, min:%s, max:%s\n", p1Sum/time.Duration(p1Limit), p1Min, p1Max)
	if p2Limit <= 0 {
		return nil
	}
	fmt.Printf("precommit2 avg:%s, min:%s, max:%s\n", p2Sum/time.Duration(p2Limit), p2Min, p2Max)
	if c1Limit <= 0 {
		return nil
	}
	fmt.Printf("commit1    avg:%s, min:%s, max:%s\n", c1Sum/time.Duration(c1Limit), c1Min, c1Max)
	if c2Limit <= 0 {
		return nil
	}
	fmt.Printf("commit2    avg:%s, min:%s, max:%s\n", c2Sum/time.Duration(c2Limit), c2Min, c2Max)

	return nil
}

func prepareTask(ctx context.Context, sb *ffiwrapper.Sealer, task *ParallelBenchTask) {
	parallelLock.Lock()
	if !canParallel(task.Type) {
		parallelLock.Unlock()
		log.Infof("parallel limitted, retry task: %s_%d", task.SectorName(), task.Type)
		go returnTask(task)
		return
	}
	offsetParallel(task, 1)
	parallelLock.Unlock()
	go runTask(ctx, sb, task)
}

func runTask(ctx context.Context, sb *ffiwrapper.Sealer, task *ParallelBenchTask) {
	defer func() {
		parallelLock.Lock()
		offsetParallel(task, -1)
		parallelLock.Unlock()
	}()
	sectorSize := task.SectorSize

	sid := storage.SectorRef{
		ID:        task.SectorID,
		ProofType: spt(sectorSize),
	}
	switch task.Type {
	case TASK_KIND_ADDPIECE:
		startTime := time.Now()
		r := rand.New(rand.NewSource(100 + int64(task.SectorID.Number)))
		pi, err := sb.AddPiece(ctx, sid, nil, abi.PaddedPieceSize(sectorSize).Unpadded(), r)
		if err != nil {
			panic(err) // failed
		}
		apResult.Store(task.SectorName(), ParallelBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		})

		newTask := *task
		newTask.Type = TASK_KIND_PRECOMMIT1
		newTask.Pieces = []abi.PieceInfo{
			pi,
		}
		if p1Limit == 0 {
			doneEvent <- &newTask
		} else {
			p1Chan <- &newTask
		}
		return

	case TASK_KIND_PRECOMMIT1:
		startTime := time.Now()
		trand := blake2b.Sum256(task.TicketPreimage)
		ticket := abi.SealRandomness(trand[:])

		var pc1o storage.PreCommit1Out
		var err error
		if !task.TaskSet {
			// if the FIL_PROOFS_MULTICORE_SDR_PRODUCERS haven't set, set it by auto.
			if len(os.Getenv("FIL_PROOFS_MULTICORE_SDR_PRODUCERS")) == 0 {
				if err := ffiwrapper.AutoPrecommit1Env(ctx); err != nil {
					panic(err)
				}
			}
			pc1o, err = sb.SealPreCommit1(ctx, sid, ticket, task.Pieces)
			if err != nil {
				panic(err)
			}
		} else {
			pc1o, err = ffiwrapper.ExecPrecommit1(ctx, task.Repo, ffiwrapper.WorkerTask{
				Type:      ffiwrapper.WorkerTaskType(task.Type),
				ProofType: sid.ProofType,
				SectorID:  sid.ID,

				// p1
				SealTicket: ticket,
				Pieces:     ffiwrapper.EncodePieceInfo(task.Pieces),
			})

		}
		p1Result.Store(task.SectorName(), ParallelBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		})

		newTask := *task
		newTask.Type = TASK_KIND_PRECOMMIT2
		newTask.PreCommit1Out = pc1o
		if p2Limit == 0 {
			doneEvent <- &newTask
		} else {
			p2Chan <- &newTask
		}
		return
	case TASK_KIND_PRECOMMIT2:
		startTime := time.Now()
		var cids storage.SectorCids
		var err error
		if !task.TaskSet {
			cids, err = sb.SealPreCommit2(ctx, sid, task.PreCommit1Out)
			if err != nil {
				panic(err)
			}
		} else {
			cids, err = ffiwrapper.ExecPrecommit2(ctx, task.Repo, ffiwrapper.WorkerTask{
				Type:      ffiwrapper.WorkerTaskType(task.Type),
				ProofType: sid.ProofType,
				SectorID:  sid.ID,

				// p2
				PreCommit1Out: task.PreCommit1Out,
			})
		}
		p2Result.Store(task.SectorName(), ParallelBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		})

		newTask := *task
		newTask.Type = TASK_KIND_COMMIT1
		newTask.Cids = cids
		if c1Limit == 0 {
			doneEvent <- &newTask
		} else {
			c1Chan <- &newTask
		}
		return
	case TASK_KIND_COMMIT1:
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
		c1Result.Store(task.SectorName(), ParallelBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		})

		newTask := *task
		newTask.Type = TASK_KIND_COMMIT2
		newTask.Commit1Out = c1o
		if c2Limit == 0 {
			doneEvent <- &newTask
		} else {
			c2Chan <- &newTask
		}
		return

	case TASK_KIND_COMMIT2:
		startTime := time.Now()
		proof, err := sb.SealCommit2(ctx, sid, task.Commit1Out)
		if err != nil {
			panic(err)
		}
		c2Result.Store(task.SectorName(), ParallelBenchResult{
			startTime: startTime,
			endTime:   time.Now(),
		})

		newTask := *task
		newTask.Type = 200
		newTask.Proof = proof
		doneEvent <- &newTask
		return
	}
	panic(fmt.Sprintf("not reach here:%d", task.Type))
}
