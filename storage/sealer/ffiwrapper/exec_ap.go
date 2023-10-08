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
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gwaylib/errors"
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

func (sb *Sealer) ExecAddPiece(ctx context.Context, task WorkerTask) (abi.PieceInfo, error) {
	log.Infow("ExecAddPiece Start", "sector", task.SectorName())
	defer log.Infow("ExecAddPiece Finish", "sector", task.SectorName())
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("ExecAddPiece panic: %v\n%v", e, string(debug.Stack()))
		}
	}()
	args, err := json.Marshal(&ExecAddPieceReq{
		Repo: sb.RepoPath(),
		Task: task,
	})
	if err != nil {
		return abi.PieceInfo{}, errors.As(err)
	}

	taskConfig, err := GetGlobalResourceManager().AllocateResource(APTask)
	if err != nil {
		return abi.PieceInfo{}, errors.As(err)
	}
	defer GetGlobalResourceManager().ReleaseResource(taskConfig)

	unixAddr := filepath.Join(os.TempDir(), fmt.Sprintf(".addpiece-%s-%s", task.Key(), taskConfig.CPUSet))
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
		return abi.PieceInfo{}, errors.As(err)
	}

	if err := BindCpuStr(cmd.Process.Pid, strings.Split(taskConfig.CPUSet, ",")); err != nil {
		cmd.Process.Kill()
		return abi.PieceInfo{}, errors.As(err)
	}
	StoreTaskPid(task.SectorName(), cmd.Process.Pid)
	defer FreeTaskPid(task.SectorName())

	log.Infof("DEBUG: bind addpiece: %+v, process:%d, cpus:%+v", task.Key(), cmd.Process.Pid, taskConfig.CPUSet)

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
		return abi.PieceInfo{}, errors.As(err)
	}
	// will block the data
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug(errors.As(err))
		}
	}()
	// write args
	encryptArg, err := AESEncrypt(args, fmt.Sprintf("%x", md5.Sum([]byte(_exec_code))))
	if err != nil {
		return abi.PieceInfo{}, errors.As(err, string(args))
	}
	if _, err := conn.Write(encryptArg); err != nil {
		return abi.PieceInfo{}, errors.As(err, string(args))
	}

	// wait resp
	out, err := readUnixConn(conn)
	if err != nil {
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
	},
	Action: func(cctx *cli.Context) error {
		//	ctx := cctx.Context
		ctx := context.Background()
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
					SectorId:       storiface.SectorName(task.SectorID),
					SealedRepo:     workerRepo,
					UnsealedRepo:   workerRepo,
					IsMarketSector: task.SectorStorage.UnsealedStorage.ID != 0,
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