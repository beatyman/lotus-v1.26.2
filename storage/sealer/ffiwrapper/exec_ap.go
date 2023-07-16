package ffiwrapper

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/hardware/bindcpu"
	"github.com/urfave/cli/v2"
)

type ExecAddPieceReq struct {
	Repo string
	Task WorkerTask
}

type ExecAddPieceResp struct {
	Data abi.PieceInfo
	Err  string
}

var (
	_addPieceUnixConn net.Conn
	_addPieceLock     = sync.Mutex{}
)

func closeAddPieceProcess() {
	if _addPieceUnixConn != nil {
		_addPieceUnixConn.Close()
	}
	_addPieceUnixConn = nil
}

func bindAddPieceProcess(ctx context.Context) error {
	aKeys, aVal,err := AllocateCpuForAP(ctx)
	defer bindcpu.ReturnCpus(aKeys)
	cpus := aVal.Cpus

	unixAddr := filepath.Join(os.TempDir(), ".addpiece")
	cmd := exec.CommandContext(ctx, os.Args[0],
		"addpiece",
		"--addr", unixAddr,
	)
	// set the env
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("LOTUS_EXEC_CODE=%s", _exec_code))
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return errors.As(err)
	}
	log.Infof("task: %+v,try bind cpu: %+v",cmd.Process.Pid,cpus)
	if err := bindcpu.BindCpu(cmd.Process.Pid, cpus); err != nil {
		cmd.Process.Kill()
		return errors.As(err)
	}

	// transfer precommit1 parameters
	var d net.Dialer
	d.LocalAddr = nil // if you have a local addr, add it here
	retryTime := 0
	raddr := net.UnixAddr{Name: unixAddr, Net: "unix"}
loopUnixConn:
	conn, err := d.DialContext(ctx, "unix", raddr.String())
	if err != nil {
		retryTime++
		if retryTime < 100 {
			time.Sleep(200 * time.Millisecond)
			goto loopUnixConn
		}
		cmd.Process.Kill()
		return errors.As(err, raddr.String())
	}
	_addPieceUnixConn = conn

	// will block the data
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug(errors.As(err))
		}
	}()
	log.Infof("DEBUG: bind addpiece, process:%d, cpus:%+v", cmd.Process.Pid, cpus)
	return nil
}

func (sb *Sealer) ExecAddPiece(ctx context.Context, task WorkerTask) (abi.PieceInfo, error) {
	args, err := json.Marshal(&ExecAddPieceReq{
		Repo: sb.RepoPath(),
		Task: task,
	})
	if err != nil {
		return abi.PieceInfo{}, errors.As(err)
	}

	_addPieceLock.Lock()
	defer _addPieceLock.Unlock()

	// bind process
	if _addPieceUnixConn == nil {
		if err := bindAddPieceProcess(ctx); err != nil {
			closeAddPieceProcess()
			return abi.PieceInfo{}, errors.As(err)
		}
	}

	// write args
	encryptArg, err := AESEncrypt(args, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		closeAddPieceProcess()
		return abi.PieceInfo{}, errors.As(err, string(args))
	}
	if _, err := _addPieceUnixConn.Write(encryptArg); err != nil {
		closeAddPieceProcess()
		return abi.PieceInfo{}, errors.As(err, string(args))
	}

	// wait resp
	out, err := readUnixConn(_addPieceUnixConn)
	if err != nil {
		closeAddPieceProcess()
		return abi.PieceInfo{}, errors.As(err, string(args))
	}
	decodeOut, err := AESDecrypt(out, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		return abi.PieceInfo{}, errors.As(err, string(args))
	}
	resp := ExecAddPieceResp{}
	if err := json.Unmarshal(decodeOut, &resp); err != nil {
		return abi.PieceInfo{}, errors.As(err, string(args), string(out))
	}
	if len(resp.Err) > 0 {
		return abi.PieceInfo{}, errors.Parse(resp.Err)
	}
	return resp.Data, nil
}

var AddPieceCmd = &cli.Command{
	Name:    "addpiece",
	Aliases: []string{"pledge"},
	Usage:   "run addpiece in process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "addr", // listen address
		},
		&cli.BoolFlag{
			Name:  "cpu-bind",
			Usage: "--cpu-bind [l3index]",
		},
		&cli.BoolFlag{
			Name:  "cpu-num",
			Usage: "--cpu-num [l3index]",
		},
	},
	Action: func(cctx *cli.Context) error {
		//	ctx := cctx.Context
		ctx := context.Background()
		if cctx.Bool("cpu-bind") {
			index, _ := strconv.Atoi(cctx.Args().First())
			l3Cpus, err := bindcpu.UseL3CPU(ctx, index)
			if err != nil {
				return errors.As(err)
			}
			fmt.Println(strings.Join(l3Cpus, ","))
			return nil
		}
		if cctx.Bool("cpu-num") {
			index, _ := strconv.Atoi(cctx.Args().First())
			l3Cpus, err := bindcpu.UseL3CPU(ctx, index)
			if err != nil {
				return errors.As(err)
			}
			if err != nil {
				return errors.As(err)
			}
			fmt.Println(len(l3Cpus))
			return nil
		}

		localCode := os.Getenv("LOTUS_EXEC_CODE")
		if len(localCode) == 0 {
			return nil
		}
		unixFile := cctx.String("addr")
		if err := os.Remove(unixFile); err != nil {
			log.Info(errors.As(err))
		}

		unixAddr := net.UnixAddr{Name: unixFile, Net: "unix"}

		// unix listen
		ln, err := net.ListenUnix("unix", &unixAddr)
		if err != nil {
			return errors.As(err)
		}
		defer ln.Close()

		conn, err := ln.Accept()
		if err != nil {
			return errors.As(err)
		}
		defer conn.Close()

		workLock := sync.Mutex{}
		work := func(argIn []byte) {
			workLock.Lock()
			defer workLock.Unlock()

			resp := ExecAddPieceResp{}
			defer func() {
				if r := recover(); r != nil {
					resp.Err = errors.New("panic").As(r).Error()
					debug.PrintStack()
				}
				result, err := json.Marshal(&resp)
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				encryptOut, err := AESEncrypt(result, fmt.Sprintf("%x", md5.Sum([]byte(localCode))))
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				if _, err := conn.Write(encryptOut); err != nil {
					log.Error(errors.As(err, string(result)))
					os.Exit(2)
					return
				}
			}()

			req := ExecAddPieceReq{}
			if err := json.Unmarshal(argIn, &req); err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}

			workerRepo := req.Repo
			task := req.Task

			log.Infof("addpiece process req in:%s, %s, %+v", unixAddr.String(), workerRepo, task.SectorID)
			workerSealer, err := New(RemoteCfg{}, &basicfs.Provider{
				Root: workerRepo,
			})
			if err != nil {
				resp.Err = errors.As(err, string(argIn)).Error()
				return
			}
			sref := storiface.SectorRef{
				ID:        task.SectorID,
				ProofType: task.ProofType,
				SectorFile: storiface.SectorFile{
					SectorId:     storiface.SectorName(task.SectorID),
					SealedRepo:   workerRepo,
					UnsealedRepo: workerRepo,
					IsMarketSector: task.SectorStorage.UnsealedStorage.ID!=0,
				},
				StoreUnseal: task.StoreUnseal,
			}
			out, err := workerSealer.AddPiece(ctx, sref, task.ExistingPieceSizes, task.PieceSize, task.PieceData)
			if err != nil {
				resp.Err = errors.As(err, task.PieceData).Error()
				return
			}
			resp.Data = out
			return
		}

		for {
			// handle connection
			argIn, err := readUnixConn(conn)
			if err != nil {
				if errors.Equal(err, io.EOF) {
					return nil
				}
				return errors.As(err)
			}
			decodeArg, err := AESDecrypt(argIn, fmt.Sprintf("%x", md5.Sum([]byte(localCode))))
			if err != nil {
				return errors.As(err)
			}
			go work(decodeArg)
		}
		return nil
	},
}
