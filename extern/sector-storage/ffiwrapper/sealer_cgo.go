//go:build cgo
// +build cgo

package ffiwrapper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	rlepluslazy "github.com/filecoin-project/go-bitfield/rle"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"

	"github.com/detailyang/go-fallocate"
	commpffi "github.com/filecoin-project/go-commp-utils/ffiwrapper"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/lotus/extern/sector-storage/fr32"
	"github.com/filecoin-project/lotus/extern/sector-storage/partialfile"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/gwaylib/errors"
)

var _ Storage = &Sealer{}

func New(remoteCfg RemoteCfg, sectors SectorProvider) (*Sealer, error) {
	sb := &Sealer{
		sectors: sectors,

		stopping: make(chan struct{}),

		remoteCfg: remoteCfg,
	}
	if sectors != nil {
		if err := os.MkdirAll(sectors.RepoPath(), 0755); err != nil {
			return nil, err
		}
	}

	return sb, nil
}

func (sb *Sealer) ExpireAllMarketRetrieve() {
	go func() {
		// unseal data will expire by 30 days if no visitor.
		overdues, err := database.GetMarketRetrieveExpires(time.Now().AddDate(0, 0, -30))
		if err != nil {
			log.Error(errors.As(err))
			return
		}

		// for unsealed
		expiredNum := 0
		for _, sFile := range overdues {
			switch sFile.UnsealedStorageType {
			case database.MOUNT_TYPE_HLM:
				// loading special storage implement
				stor, err := database.GetStorage(sFile.UnsealedStorageId)
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				ctx := context.TODO()
				sid := sFile.SectorId
				auth := hlmclient.NewAuthClient(stor.MountAuthUri, stor.MountAuth)
				token, err := auth.NewFileToken(ctx, sid)
				if err != nil {
					log.Error(errors.As(err))
					return
				}
				if err := hlmclient.NewHttpClient(stor.MountTransfUri, sid, string(token)).DeleteSector(ctx, sid, "unsealed"); err != nil {
					log.Error(errors.As(err))
					return
				}
			default:
				if len(sFile.UnsealedRepo) > 0 {
					log.Warnf("remove unseal:%s", sFile.UnsealedFile())
					err = os.RemoveAll(sFile.UnsealedFile())
					if err != nil {
						log.Error(errors.As(err))
						break
					}
				}
			}

			// remove miner link
			minerRepo := sb.RepoPath()
			if len(minerRepo) > 0 {
				file := filepath.Join(minerRepo, "unsealed", sFile.SectorId)
				log.Warnf("remove unseal:%s", file)
				err = os.RemoveAll(file)
				if err != nil {
					log.Error(errors.As(err))
					break
				}
			}
			expiredNum++
		}

		if expiredNum > 0 {
			log.Warnf("expired unsealed total:%d", expiredNum)
		}
	}()
}

func (sb *Sealer) NewSector(ctx context.Context, sector storage.SectorRef) error {
	log.Infof("sealer.NewSector:%+v, market:%t", sector.ID, sector.IsMarketSector)

	if database.HasDB() {
		sName := sectorName(sector.ID)
		unsealedStorageId := int64(0)
		state := -1
		if sector.IsMarketSector {
			tx, unsealedStorage, err := database.PrepareStorage(sName, "", database.STORAGE_KIND_UNSEALED)
			if err != nil {
				return errors.As(err)
			}
			if err := tx.Commit(); err != nil {
				tx.Rollback()
				return errors.As(err)
			}
			unsealedStorageId = unsealedStorage.ID
			state = database.SECTOR_STATE_PLEDGE // no clean when plege failed. -1 will clean the unsealed data when failed.

			// expire unsealed to control the unseal space when a new unseal creating.
			sb.ExpireAllMarketRetrieve()
		}
		now := time.Now()
		seInfo := &database.SectorInfo{
			ID:              sName,
			MinerId:         fmt.Sprintf("s-t0%d", sector.ID.Miner),
			UpdateTime:      now,
			StorageUnsealed: unsealedStorageId,
			State:           state,
			StateTime:       now,
			CreateTime:      now,
		}
		if err := database.AddSectorInfo(seInfo); err != nil {
			return errors.As(err)
		}
	}

	return nil
}

func (sb *Sealer) AddPiece(ctx context.Context, sector storage.SectorRef, existingPieceSizes []abi.UnpaddedPieceSize, pieceSize abi.UnpaddedPieceSize, file storage.Data) (abi.PieceInfo, error) {
	// uprade SectorRef
	var err error
	sector, err = database.FillSectorFile(sector, sb.RepoPath())
	if err != nil {
		return abi.PieceInfo{}, errors.As(err)
	}

	path := filepath.Join(sb.RepoPath(), "sector_cc")
	pathTxt := filepath.Join(sb.RepoPath(), "sector_cc.txt")
	//是否空单数据
	log.Info("secors =============", sector)
	log.Info("existingPieceSizes len =============", len(existingPieceSizes))
	log.Info("pieceSize==== =============", pieceSize)
	log.Info("file========= =============", file)
	isSectorCc := false
	if len(existingPieceSizes) == 0 {
		isSectorCc = true
	}
	//当miner为有效订单是 isSectorCc 设置为false
	if sector.SectorFile.IsMarketSector {
		log.Info("sector.SectorFile.IsMarketSector Eff order===========================================", sector.SectorFile.IsMarketSector, sector.ID)
		isSectorCc = false
	}
	//判断扇区是否是空单数据
	log.Info("SecotrId _ isSectorCc====================", sector.ID, isSectorCc)
	if isSectorCc {
		//判断空单文件是否存在
		isExist, err := PathExists(pathTxt)
		if err != nil {
			log.Error("AddPiece PathExists Err :", err)
		}
		//判断是否存在cc文件，如果存在跳过AddPiece 直接返回cid
		if isExist {
			//拷贝文件到对应的unseal目录
			log.Info("sector.UnsealedRepo===================================", sector.UnsealedRepo)
			CopyFile(path, sector.UnsealedFile())
			log.Info("AddPiece Skip By SectorId:", sector.ID)
			cid1, err := ReadSecorCC(pathTxt)
			if err != nil {
				log.Error("AddPiece error ReadSecorCC ===========", err)
			}
			if err == nil {
				return abi.PieceInfo{
					Size:     pieceSize.Padded(),
					PieceCID: cid1,
				}, nil
			}
		}
	}
	log.Info("Execute AddPiece Task ", sector.ID, isSectorCc)

	// TODO: allow tuning those:
	chunk := abi.PaddedPieceSize(4 << 20)
	parallel := runtime.NumCPU()

	var offset abi.UnpaddedPieceSize
	for _, size := range existingPieceSizes {
		offset += size
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return abi.PieceInfo{}, err
	}

	maxPieceSize := abi.PaddedPieceSize(ssize)

	if offset.Padded()+pieceSize.Padded() > maxPieceSize {
		return abi.PieceInfo{}, xerrors.Errorf("can't add %d byte piece to sector %v with %d bytes of existing pieces", pieceSize, sector, offset)
	}

	var done func()
	var stagedFile *partialfile.PartialFile

	defer func() {
		if done != nil {
			done()
		}

		if stagedFile != nil {
			if err := stagedFile.Close(); err != nil {
				log.Errorf("closing staged file: %+v", err)
			}
		}
	}()

	unsealedPath := sector.UnsealedFile()
	if err := os.MkdirAll(filepath.Dir(unsealedPath), 0755); err != nil {
		return abi.PieceInfo{}, xerrors.Errorf("creating unsealed sector file: %w", err)
	}
	if len(existingPieceSizes) == 0 {
		stagedFile, err = partialfile.CreateUnsealedPartialFile(maxPieceSize, sector)
		//拷贝文件
		if isSectorCc {
			log.Info("copyFile path ====================", path)
			isSuccess := CopyFile(unsealedPath, path)
			if !isSuccess {
				log.Error("Copy Unsealed File Failt .....")
			}
		}
		if err != nil {
			return abi.PieceInfo{}, xerrors.Errorf("creating unsealed sector file: %w", err)
		}
	} else {
		log.Info("===============================================Effective order======", sector.ID)
		stagedFile, err = partialfile.OpenUnsealedPartialFile(maxPieceSize, sector)
		if err != nil {
			return abi.PieceInfo{}, xerrors.Errorf("opening unsealed sector file: %w", err)
		}
	}
	w, err := stagedFile.Writer(storiface.UnpaddedByteIndex(offset).Padded(), pieceSize.Padded())
	if err != nil {
		return abi.PieceInfo{}, xerrors.Errorf("getting partial file writer: %w", err)
	}

	pw := fr32.NewPadWriter(w)

	pr := io.TeeReader(io.LimitReader(file, int64(pieceSize)), pw)

	throttle := make(chan []byte, parallel)
	piecePromises := make([]func() (abi.PieceInfo, error), 0)

	buf := make([]byte, chunk.Unpadded())
	for i := 0; i < parallel; i++ {
		if abi.UnpaddedPieceSize(i)*chunk.Unpadded() >= pieceSize {
			break // won't use this many buffers
		}
		throttle <- make([]byte, chunk.Unpadded())
	}

	for {
		var read int
		for rbuf := buf; len(rbuf) > 0; {
			n, err := pr.Read(rbuf)
			if err != nil && err != io.EOF {
				return abi.PieceInfo{}, xerrors.Errorf("pr read error: %w", err)
			}

			rbuf = rbuf[n:]
			read += n

			if err == io.EOF {
				break
			}
		}
		if read == 0 {
			break
		}

		done := make(chan struct {
			cid.Cid
			error
		}, 1)
		pbuf := <-throttle
		copy(pbuf, buf[:read])

		go func(read int) {
			defer func() {
				throttle <- pbuf
			}()

			c, err := sb.pieceCid(sector.ProofType, pbuf[:read])
			done <- struct {
				cid.Cid
				error
			}{c, err}
		}(read)

		piecePromises = append(piecePromises, func() (abi.PieceInfo, error) {
			select {
			case e := <-done:
				if e.error != nil {
					return abi.PieceInfo{}, e.error
				}

				return abi.PieceInfo{
					Size:     abi.UnpaddedPieceSize(len(buf[:read])).Padded(),
					PieceCID: e.Cid,
				}, nil
			case <-ctx.Done():
				return abi.PieceInfo{}, ctx.Err()
			}
		})
	}

	if err := pw.Close(); err != nil {
		return abi.PieceInfo{}, xerrors.Errorf("closing padded writer: %w", err)
	}

	if err := stagedFile.MarkAllocated(storiface.UnpaddedByteIndex(offset).Padded(), pieceSize.Padded()); err != nil {
		return abi.PieceInfo{}, xerrors.Errorf("marking data range as allocated: %w", err)
	}

	if err := stagedFile.Close(); err != nil {
		return abi.PieceInfo{}, err
	}
	stagedFile = nil

	if len(piecePromises) == 1 {
		piece, _ := piecePromises[0]()
		if isSectorCc {
			//格式化json cid 写入磁盘中
			pieceJson, _ := piece.PieceCID.MarshalJSON()
			err = WriteSectorCC(pathTxt, pieceJson)
			if err != nil {
				log.Error("=============================WriteSectorCC ===err: ", err)
			}
		}
		return abi.PieceInfo{
			Size:     pieceSize.Padded(),
			PieceCID: piece.PieceCID,
		}, nil
	}

	var payloadRoundedBytes abi.PaddedPieceSize
	pieceCids := make([]abi.PieceInfo, len(piecePromises))
	for i, promise := range piecePromises {
		pinfo, err := promise()
		if err != nil {
			return abi.PieceInfo{}, err
		}

		pieceCids[i] = pinfo
		payloadRoundedBytes += pinfo.Size
	}

	pieceCID, err := ffi.GenerateUnsealedCID(sector.ProofType, pieceCids)
	if err != nil {
		return abi.PieceInfo{}, xerrors.Errorf("generate unsealed CID: %w", err)
	}

	// validate that the pieceCID was properly formed
	if _, err := commcid.CIDToPieceCommitmentV1(pieceCID); err != nil {
		return abi.PieceInfo{}, err
	}

	if payloadRoundedBytes < pieceSize.Padded() {
		paddedCid, err := commpffi.ZeroPadPieceCommitment(pieceCID, payloadRoundedBytes.Unpadded(), pieceSize)
		if err != nil {
			return abi.PieceInfo{}, xerrors.Errorf("failed to pad data: %w", err)
		}

		pieceCID = paddedCid
	}

	if isSectorCc {
		//格式化json cid 写入磁盘中
		bytes, err := pieceCID.MarshalJSON()
		err = WriteSectorCC(pathTxt, bytes)
		if err != nil {
			log.Error("WriteSector CC err : ", err)
		}
	}

	return abi.PieceInfo{
		Size:     pieceSize.Padded(),
		PieceCID: pieceCID,
	}, nil
}

func (sb *Sealer) pieceCid(spt abi.RegisteredSealProof, in []byte) (cid.Cid, error) {
	prf, werr, err := commpffi.ToReadableFile(bytes.NewReader(in), int64(len(in)))
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting tee reader pipe: %w", err)
	}

	pieceCID, err := ffi.GeneratePieceCIDFromFile(spt, prf, abi.UnpaddedPieceSize(len(in)))
	if err != nil {
		return cid.Undef, xerrors.Errorf("generating piece commitment: %w", err)
	}

	_ = prf.Close()

	return pieceCID, werr()
}
func (sb *Sealer) tryDecodeUpdatedReplica(ctx context.Context, sector storage.SectorRef, commD cid.Cid, unsealedPath string) (bool, error) {
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUpdate|storiface.FTSealed|storiface.FTCache, storiface.FTNone, storiface.PathStorage)
	if xerrors.Is(err, storiface.ErrSectorNotFound) {
		return false, nil
	} else if err != nil {
		return false, xerrors.Errorf("reading updated replica: %w", err)
	}
	defer done()

	// Sector data stored in replica update
	updateProof, err := sector.ProofType.RegisteredUpdateProof()
	if err != nil {
		return false, err
	}
	return true, ffi.SectorUpdate.DecodeFrom(updateProof, unsealedPath, paths.Update, paths.Sealed, paths.Cache, commD)
}

func (sb *Sealer) unsealPiece(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize, randomness abi.SealRandomness, commd cid.Cid) error {
	log.Infof("DEBUG:unsealPiece in, sector:%+v", sector)
	defer log.Infof("DEBUG:unsealPiece out, sector:%+v", sector)

	// uprade SectorRef
	var err error
	sector, err = database.FillSectorFile(sector, sb.RepoPath())
	if err != nil {
		return errors.As(err)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)

	// implements from hlm
	sPath := storiface.SectorPaths{
		ID:       sector.ID,
		Unsealed: sector.UnsealedFile(),
		Sealed:   sector.SealedFile(),
		Cache:    sector.CachePath(),
	}
	var pf *partialfile.PartialFile

	_, err = os.Stat(sPath.Unsealed)
	if err != nil {
		if !os.IsNotExist(err) {
			return xerrors.Errorf("acquire unsealed sector path (existing): %w", err)
		}

		pf, err = partialfile.CreateUnsealedPartialFile(maxPieceSize, sector)
		if err != nil {
			return xerrors.Errorf("create unsealed file: %w", err)
		}
	} else {
		pf, err = partialfile.OpenUnsealedPartialFile(maxPieceSize, sector)
		if err != nil {
			return xerrors.Errorf("opening partial file: %w", err)
		}
	}
	defer pf.Close() // nolint

	//	// try finding existing
	//	unsealedPath, done, err := sb.sectors.AcquireSector(ctx, sector, stores.FTUnsealed, stores.FTNone, stores.PathStorage)
	//	var pf *partialFile
	//
	//	switch {
	//	case xerrors.Is(err, storiface.ErrSectorNotFound):
	//		unsealedPath, done, err = sb.sectors.AcquireSector(ctx, sector, stores.FTNone, stores.FTUnsealed, stores.PathStorage)
	//		if err != nil {
	//			return xerrors.Errorf("acquire unsealed sector path (allocate): %w", err)
	//		}
	//		defer done()
	//
	//		pf, err = createPartialFile(maxPieceSize, unsealedPath.Unsealed)
	//		if err != nil {
	//			return xerrors.Errorf("create unsealed file: %w", err)
	//		}
	//
	//	case err == nil:
	//		defer done()
	//
	//		pf, err = openPartialFile(maxPieceSize, unsealedPath.Unsealed)
	//		if err != nil {
	//			return xerrors.Errorf("opening partial file: %w", err)
	//		}
	//	default:
	//		return xerrors.Errorf("acquire unsealed sector path (existing): %w", err)
	//	}
	//	defer pf.Close() // nolint

	allocated, err := pf.Allocated()
	if err != nil {
		return xerrors.Errorf("getting bitruns of allocated data: %w", err)
	}

	toUnseal, err := computeUnsealRanges(allocated, offset, size)
	if err != nil {
		return xerrors.Errorf("computing unseal ranges: %w", err)
	}

	if !toUnseal.HasNext() {
		return nil
	}
	// If piece data stored in updated replica decode whole sector
	decoded, err := sb.tryDecodeUpdatedReplica(ctx, sector, commd, sPath.Unsealed)
	if err != nil {
		return xerrors.Errorf("decoding sector from replica: %w", err)
	}
	if decoded {
		return pf.MarkAllocated(0, maxPieceSize)
	}

	// Piece data sealed in sector
	//srcPaths, srcDone, err := sb.sectors.AcquireSector(ctx, sector, stores.FTCache|stores.FTSealed, stores.FTNone, stores.PathStorage)
	//if err != nil {
	//	return xerrors.Errorf("acquire sealed sector paths: %w", err)
	//}
	//defer srcDone()
	up := os.Getenv("US3")
	var sealed *os.File
	if up != "" {
		d := operation.NewDownloaderV2()
		sealed, err = d.DownloadFile(sPath.Sealed, sPath.Sealed)
		if err != nil {
			return xerrors.Errorf("opening sealed file: %w", err)
		}
		defer sealed.Close() // nolint
	} else {
		sealed, err = os.OpenFile(sPath.Sealed, os.O_RDONLY, 0644) // nolint:gosec
		if err != nil {
			return xerrors.Errorf("opening sealed file: %w", err)
		}
		defer sealed.Close() // nolint
	}

	var at, nextat abi.PaddedPieceSize
	first := true
	for first || toUnseal.HasNext() {
		first = false

		piece, err := toUnseal.NextRun()
		if err != nil {
			return xerrors.Errorf("getting next range to unseal: %w", err)
		}

		at = nextat
		nextat += abi.PaddedPieceSize(piece.Len)

		if !piece.Val {
			continue
		}

		out, err := pf.Writer(offset.Padded(), size.Padded())
		if err != nil {
			return xerrors.Errorf("getting partial file writer: %w", err)
		}

		// <eww>
		opr, opw, err := os.Pipe()
		if err != nil {
			return xerrors.Errorf("creating out pipe: %w", err)
		}

		var perr error
		outWait := make(chan struct{})

		{
			go func() {
				defer close(outWait)
				defer opr.Close() // nolint

				padwriter := fr32.NewPadWriter(out)

				bsize := uint64(size.Padded())
				if bsize > uint64(runtime.NumCPU())*fr32.MTTresh {
					bsize = uint64(runtime.NumCPU()) * fr32.MTTresh
				}

				bw := bufio.NewWriterSize(padwriter, int(abi.PaddedPieceSize(bsize).Unpadded()))

				_, err := io.CopyN(bw, opr, int64(size))
				if err != nil {
					perr = xerrors.Errorf("copying data: %w", err)
					return
				}

				if err := bw.Flush(); err != nil {
					perr = xerrors.Errorf("flushing unpadded data: %w", err)
					return
				}

				if err := padwriter.Close(); err != nil {
					perr = xerrors.Errorf("closing padwriter: %w", err)
					return
				}
			}()
		}
		// </eww>

		// TODO: This may be possible to do in parallel
		err = ffi.UnsealRange(sector.ProofType,
			sPath.Cache,
			sealed,
			opw,
			sector.ID.Number,
			sector.ID.Miner,
			randomness,
			commd,
			uint64(at.Unpadded()),
			uint64(abi.PaddedPieceSize(piece.Len).Unpadded()))

		_ = opw.Close()

		if err != nil {
			return xerrors.Errorf("unseal range: %w", err)
		}

		select {
		case <-outWait:
		case <-ctx.Done():
			return ctx.Err()
		}

		if perr != nil {
			return xerrors.Errorf("piping output to unsealed file: %w", perr)
		}

		if err := pf.MarkAllocated(storiface.PaddedByteIndex(at), abi.PaddedPieceSize(piece.Len)); err != nil {
			return xerrors.Errorf("marking unsealed range as allocated: %w", err)
		}

		if !toUnseal.HasNext() {
			break
		}
	}

	return nil
}
func (sb *Sealer) ReadPieceQiniu(ctx context.Context, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (io.ReadCloser, bool, error) {
	p, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUnsealed, storiface.FTNone, storiface.PathStorage)
	if err != nil {
		return nil, false, xerrors.Errorf("acquire unsealed sector path: %w", err)
	}
	defer done()
	sp := filepath.Join(partialfile.QINIU_VIRTUAL_MOUNTPOINT, fmt.Sprintf("s-t0%d-%d", sector.ID.Miner, sector.ID.Number))
	p.Cache = filepath.Join(sp, storiface.FTCache.String(), storiface.SectorName(sector.ID))
	p.Sealed = filepath.Join(sp, storiface.FTSealed.String(), storiface.SectorName(sector.ID))
	p.Unsealed = filepath.Join(sp, storiface.FTUnsealed.String(), storiface.SectorName(sector.ID))
	return partialfile.ReadPieceQiniu(ctx, p.Unsealed, sector, offset, size)
}
func (sb *Sealer) PieceReader(ctx context.Context, sector storage.SectorRef, offset, size abi.PaddedPieceSize) (func(startOffsetAligned storiface.PaddedByteIndex) (io.ReadCloser, error), bool, error) {
	up := os.Getenv("US3")
	if up != "" {
		return func(startOffsetAligned storiface.PaddedByteIndex) (io.ReadCloser, error) {
			sp := filepath.Join(partialfile.QINIU_VIRTUAL_MOUNTPOINT, fmt.Sprintf("s-t0%d-%d", sector.ID.Miner, sector.ID.Number))
			unsealed := filepath.Join(sp, storiface.FTUnsealed.String(), storiface.SectorName(sector.ID))
			index := storiface.UnpaddedByteIndex(storiface.PaddedByteIndex(offset) + startOffsetAligned)
			offset := size - abi.PaddedPieceSize(startOffsetAligned)
			r, _, err := partialfile.ReadPieceQiniu(ctx, unsealed, sector, index, offset.Unpadded())
			if err != nil {
				log.Error(err)
				return nil, err
			}
			return struct {
				io.Reader
				io.Closer
			}{
				Reader: r,
				Closer: funcCloser(func() error {
					return nil
				}),
			}, nil
		}, true, nil
	}
	log.Infof("DEBUG:PieceReader in, sector:%+v", sector)
	defer log.Infof("DEBUG:PieceReader out, sector:%+v", sector)
	// uprade SectorRef
	var err error
	sector, err = database.FillSectorFile(sector, sb.RepoPath())
	if err != nil {
		return nil, false, errors.As(err)
	}
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, false, err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)
	pf, err := partialfile.OpenUnsealedPartialFile(maxPieceSize, sector)
	if err != nil {
		if xerrors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, xerrors.Errorf("opening partial file: %w", err)
	}
	ok, err := pf.HasAllocated(storiface.UnpaddedByteIndex(offset.Unpadded()), size.Unpadded())
	if err != nil {
		_ = pf.Close()
		return nil, false, err
	}
	if !ok {
		_ = pf.Close()
		return nil, false, nil
	}
	return func(startOffsetAligned storiface.PaddedByteIndex) (io.ReadCloser, error) {
		r, err := pf.Reader(storiface.PaddedByteIndex(offset)+startOffsetAligned, size-abi.PaddedPieceSize(startOffsetAligned))
		if err != nil {
			_ = pf.Close()
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{
			Reader: r,
			Closer: funcCloser(func() error {
				// if we already have a reader cached, close this one
				_ = pf.Close()
				return nil
			}),
		}, nil
	}, true, nil
}

type funcCloser func() error

func (f funcCloser) Close() error {
	return f()
}
func (sb *Sealer) ReadPiece(ctx context.Context, writer io.Writer, sector storage.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	log.Infof("DEBUG:ReadPiece in, sector:%+v", sector)
	defer log.Infof("DEBUG:ReadPiece out, sector:%+v", sector)
	return true, nil
}

func (sb *Sealer) sealPreCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, pieces []abi.PieceInfo) (out storage.PreCommit1Out, err error) {
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUnsealed, storiface.FTSealed|storiface.FTCache, storiface.PathSealing)
	if err != nil {
		return nil, xerrors.Errorf("acquiring sector paths(%s): %w", sb.sectors.RepoPath(), err)
	}
	defer done()

	e, err := os.OpenFile(paths.Sealed, os.O_RDWR|os.O_CREATE, 0644) // nolint:gosec
	if err != nil {
		return nil, xerrors.Errorf("ensuring sealed file exists: %w", err)
	}
	if err := e.Close(); err != nil {
		return nil, err
	}

	if err := os.Mkdir(paths.Cache, 0755); err != nil { // nolint
		if os.IsExist(err) {
			log.Warnf("existing cache in %s; removing", paths.Cache)

			if err := os.RemoveAll(paths.Cache); err != nil {
				return nil, xerrors.Errorf("remove existing sector cache from %s (sector %d): %w", paths.Cache, sector, err)
			}

			if err := os.Mkdir(paths.Cache, 0755); err != nil { // nolint:gosec
				return nil, xerrors.Errorf("mkdir cache path after cleanup: %w", err)
			}
		} else {
			return nil, err
		}
	}

	var sum abi.UnpaddedPieceSize
	for _, piece := range pieces {
		sum += piece.Size.Unpadded()
	}
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, err
	}
	ussize := abi.PaddedPieceSize(ssize).Unpadded()
	if sum != ussize {
		return nil, xerrors.Errorf("aggregated piece sizes don't match sector size: %d != %d (%d)", sum, ussize, int64(ussize-sum))
	}

	// TODO: context cancellation respect
	p1o, err := ffi.SealPreCommitPhase1(
		sector.ProofType,
		paths.Cache,
		paths.Unsealed,
		paths.Sealed,
		sector.ID.Number,
		sector.ID.Miner,
		ticket,
		pieces,
	)
	if err != nil {
		return nil, xerrors.Errorf("presealing sector %d (%s): %w", sector.ID.Number, paths.Unsealed, err)
	}

	p1odec := map[string]interface{}{}
	if err := json.Unmarshal(p1o, &p1odec); err != nil {
		return nil, xerrors.Errorf("unmarshaling pc1 output: %w", err)
	}

	p1odec["_lotus_SealRandomness"] = ticket

	return json.Marshal(&p1odec)
}

var PC2CheckRounds = 3

func (sb *Sealer) sealPreCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage.PreCommit1Out) (storage.SectorCids, error) {

	AssertGPU(ctx)

	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTSealed|storiface.FTCache, 0, storiface.PathSealing)
	if err != nil {
		return storage.SectorCids{}, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer done()

	sealedCID, unsealedCID, err := ffi.SealPreCommitPhase2(phase1Out, paths.Cache, paths.Sealed)
	if err != nil {
		return storage.SectorCids{}, xerrors.Errorf("presealing sector %d (%s): %w", sector.ID.Number, paths.Unsealed, err)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return storage.SectorCids{}, xerrors.Errorf("get ssize: %w", err)
	}

	p1odec := map[string]interface{}{}
	if err := json.Unmarshal(phase1Out, &p1odec); err != nil {
		return storage.SectorCids{}, xerrors.Errorf("unmarshaling pc1 output: %w", err)
	}

	var ticket abi.SealRandomness
	ti, found := p1odec["_lotus_SealRandomness"]

	if found {
		ticket, err = base64.StdEncoding.DecodeString(ti.(string))
		if err != nil {
			return storage.SectorCids{}, xerrors.Errorf("decoding ticket: %w", err)
		}

		for i := 0; i < PC2CheckRounds; i++ {
			var sd [32]byte
			_, _ = rand.Read(sd[:])

			_, err := ffi.SealCommitPhase1(
				sector.ProofType,
				sealedCID,
				unsealedCID,
				paths.Cache,
				paths.Sealed,
				sector.ID.Number,
				sector.ID.Miner,
				ticket,
				sd[:],
				[]abi.PieceInfo{{Size: abi.PaddedPieceSize(ssize), PieceCID: unsealedCID}},
			)
			if err != nil {
				log.Warn("checking PreCommit failed: ", err)
				log.Warnf("num:%d tkt:%v seed:%v sealedCID:%v, unsealedCID:%v", sector.ID.Number, ticket, sd[:], sealedCID, unsealedCID)

				return storage.SectorCids{}, xerrors.Errorf("checking PreCommit failed: %w", err)
			}
		}
	}

	return storage.SectorCids{
		Unsealed: unsealedCID,
		Sealed:   sealedCID,
	}, nil
}

func (sb *Sealer) SealCommit1(ctx context.Context, sector storage.SectorRef, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness, pieces []abi.PieceInfo, cids storage.SectorCids) (storage.Commit1Out, error) {
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTSealed|storiface.FTCache, 0, storiface.PathSealing)
	if err != nil {
		return nil, xerrors.Errorf("acquire sector paths: %w", err)
	}
	defer done()
	output, err := ffi.SealCommitPhase1(
		sector.ProofType,
		cids.Sealed,
		cids.Unsealed,
		paths.Cache,
		paths.Sealed,
		sector.ID.Number,
		sector.ID.Miner,
		ticket,
		seed,
		pieces,
	)
	if err != nil {
		log.Warn("StandaloneSealCommit error: ", err)
		log.Warnf("num:%d tkt:%v seed:%v, pi:%v sealedCID:%v, unsealedCID:%v", sector.ID.Number, ticket, seed, pieces, cids.Sealed, cids.Unsealed)

		return nil, xerrors.Errorf("StandaloneSealCommit: %w", err)
	}
	return output, nil
}

func (sb *Sealer) SealCommit2(ctx context.Context, sector storage.SectorRef, phase1Out storage.Commit1Out) (storage.Proof, error) {
	AssertGPU(ctx)

	return ffi.SealCommitPhase2(phase1Out, sector.ID.Number, sector.ID.Miner)
}

type req struct {
	Path   string `json:"path"`
	ToPath string `json:"topath"`
}

func newReq(s, s1 string) *req {
	return &req{
		Path:   s,
		ToPath: s1,
	}
}

func (sb *Sealer) GenerateSectorKeyFromData(ctx context.Context, sector storage.SectorRef, commD cid.Cid) error {
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUnsealed|storiface.FTCache|storiface.FTUpdate|storiface.FTUpdateCache, storiface.FTSealed, storiface.PathSealing)
	defer done()
	if err != nil {
		return xerrors.Errorf("failed to acquire sector paths: %w", err)
	}

	s, err := os.Stat(paths.Update)
	if err != nil {
		return xerrors.Errorf("measuring update file size: %w", err)
	}
	sealedSize := s.Size()
	e, err := os.OpenFile(paths.Sealed, os.O_RDWR|os.O_CREATE, 0644) // nolint:gosec
	if err != nil {
		return xerrors.Errorf("ensuring sector key file exists: %w", err)
	}
	if err := fallocate.Fallocate(e, 0, sealedSize); err != nil {
		return xerrors.Errorf("allocating space for sector key file: %w", err)
	}
	if err := e.Close(); err != nil {
		return err
	}

	updateProofType := abi.SealProofInfos[sector.ProofType].UpdateProof
	return ffi.SectorUpdate.RemoveData(updateProofType, paths.Sealed, paths.Cache, paths.Update, paths.UpdateCache, paths.Unsealed, commD)
}

func (sb *Sealer) ReleaseSealed(ctx context.Context, sector storage.SectorRef) error {
	return xerrors.Errorf("not supported at this layer")
}

func (sb *Sealer) freeUnsealed(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage.Range) error {
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return err
	}
	maxPieceSize := abi.PaddedPieceSize(ssize)

	if len(keepUnsealed) > 0 {

		sr := partialfile.PieceRun(0, maxPieceSize)

		for _, s := range keepUnsealed {
			si := &rlepluslazy.RunSliceIterator{}
			if s.Offset != 0 {
				si.Runs = append(si.Runs, rlepluslazy.Run{Val: false, Len: uint64(s.Offset)})
			}
			si.Runs = append(si.Runs, rlepluslazy.Run{Val: true, Len: uint64(s.Size)})

			var err error
			sr, err = rlepluslazy.Subtract(sr, si)
			if err != nil {
				return err
			}
		}

		pf, err := partialfile.OpenUnsealedPartialFile(maxPieceSize, sector)
		if err == nil {
			var at uint64
			for sr.HasNext() {
				r, err := sr.NextRun()
				if err != nil {
					_ = pf.Close()
					return err
				}

				offset := at
				at += r.Len
				if !r.Val {
					continue
				}

				err = pf.Free(storiface.PaddedByteIndex(abi.UnpaddedPieceSize(offset).Padded()), abi.UnpaddedPieceSize(r.Len).Padded())
				if err != nil {
					_ = pf.Close()
					return xerrors.Errorf("free partial file range: %w", err)
				}
			}

			if err := pf.Close(); err != nil {
				return err
			}
		} else {
			if !xerrors.Is(err, os.ErrNotExist) {
				return xerrors.Errorf("opening partial file: %w", err)
			}
		}

	}

	return nil
}

func (sb *Sealer) finalizeSector(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage.Range) error {
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return err
	}

	if err := sb.freeUnsealed(ctx, sector, keepUnsealed); err != nil {
		return err
	}

	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTCache, 0, storiface.PathStorage)
	if err != nil {
		return xerrors.Errorf("acquiring sector cache path: %w", err)
	}
	defer done()

	return ffi.ClearCache(uint64(ssize), paths.Cache)
}

func (sb *Sealer) replicaUpdate(ctx context.Context, sector storage.SectorRef, pieces []abi.PieceInfo) (storage.ReplicaUpdateOut, error) {
	empty := storage.ReplicaUpdateOut{}
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUnsealed|storiface.FTSealed|storiface.FTCache, storiface.FTUpdate|storiface.FTUpdateCache, storiface.PathSealing)
	if err != nil {
		return empty, xerrors.Errorf("failed to acquire sector paths: %w", err)
	}
	defer done()

	updateProofType := abi.SealProofInfos[sector.ProofType].UpdateProof

	s, err := os.Stat(paths.Sealed)
	if err != nil {
		return empty, err
	}
	sealedSize := s.Size()

	u, err := os.OpenFile(paths.Update, os.O_RDWR|os.O_CREATE, 0644) // nolint:gosec
	if err != nil {
		return empty, xerrors.Errorf("ensuring updated replica file exists: %w", err)
	}
	if err := fallocate.Fallocate(u, 0, sealedSize); err != nil {
		return empty, xerrors.Errorf("allocating space for replica update file: %w", err)
	}

	if err := u.Close(); err != nil {
		return empty, err
	}

	if err := os.Mkdir(paths.UpdateCache, 0755); err != nil { // nolint
		if os.IsExist(err) {
			log.Warnf("existing cache in %s; removing", paths.UpdateCache)

			if err := os.RemoveAll(paths.UpdateCache); err != nil {
				return empty, xerrors.Errorf("remove existing sector cache from %s (sector %d): %w", paths.UpdateCache, sector, err)
			}

			if err := os.Mkdir(paths.UpdateCache, 0755); err != nil { // nolint:gosec
				return empty, xerrors.Errorf("mkdir cache path after cleanup: %w", err)
			}
		} else {
			return empty, err
		}
	}
	sealed, unsealed, err := ffi.SectorUpdate.EncodeInto(updateProofType, paths.Update, paths.UpdateCache, paths.Sealed, paths.Cache, paths.Unsealed, pieces)
	if err != nil {
		return empty, xerrors.Errorf("failed to update replica %d with new deal data: %w", sector.ID.Number, err)
	}
	return storage.ReplicaUpdateOut{NewSealed: sealed, NewUnsealed: unsealed}, nil
}

func (sb *Sealer) ProveReplicaUpdate1(ctx context.Context, sector storage.SectorRef, sectorKey, newSealed, newUnsealed cid.Cid) (storage.ReplicaVanillaProofs, error) {
	paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTSealed|storiface.FTCache|storiface.FTUpdate|storiface.FTUpdateCache, storiface.FTNone, storiface.PathSealing)
	if err != nil {
		return nil, xerrors.Errorf("failed to acquire sector paths: %w", err)
	}
	defer done()

	updateProofType := abi.SealProofInfos[sector.ProofType].UpdateProof

	vanillaProofs, err := ffi.SectorUpdate.GenerateUpdateVanillaProofs(updateProofType, sectorKey, newSealed, newUnsealed, paths.Update, paths.UpdateCache, paths.Sealed, paths.Cache)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate proof of replica update for sector %d: %w", sector.ID.Number, err)
	}
	return vanillaProofs, nil
}

func (sb *Sealer) ProveReplicaUpdate2(ctx context.Context, sector storage.SectorRef, sectorKey, newSealed, newUnsealed cid.Cid, vanillaProofs storage.ReplicaVanillaProofs) (storage.ReplicaUpdateProof, error) {
	updateProofType := abi.SealProofInfos[sector.ProofType].UpdateProof
	return ffi.SectorUpdate.GenerateUpdateProofWithVanilla(updateProofType, sectorKey, newSealed, newUnsealed, vanillaProofs)
}

func (sb *Sealer) finalizeReplicaUpdate(ctx context.Context, sector storage.SectorRef, keepUnsealed []storage.Range) error {
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return err
	}

	if err := sb.freeUnsealed(ctx, sector, keepUnsealed); err != nil {
		return err
	}

	{
		paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTCache, 0, storiface.PathStorage)
		if err != nil {
			return xerrors.Errorf("acquiring sector cache path: %w", err)
		}
		defer done()

		if err := ffi.ClearCache(uint64(ssize), paths.Cache); err != nil {
			return xerrors.Errorf("clear cache: %w", err)
		}
	}

	{
		paths, done, err := sb.sectors.AcquireSector(ctx, sector, storiface.FTUpdateCache, 0, storiface.PathStorage)
		if err != nil {
			return xerrors.Errorf("acquiring sector cache path: %w", err)
		}
		defer done()

		if err := ffi.ClearCache(uint64(ssize), paths.UpdateCache); err != nil {
			return xerrors.Errorf("clear cache: %w", err)
		}
	}

	return nil
}

func (sb *Sealer) ReleaseUnsealed(ctx context.Context, sector storage.SectorRef, safeToFree []storage.Range) error {
	// This call is meant to mark storage as 'freeable'. Given that unsealing is
	// very expensive, we don't remove data as soon as we can - instead we only
	// do that when we don't have free space for data that really needs it

	// This function should not be called at this layer, everything should be
	// handled in localworker
	return xerrors.Errorf("not supported at this layer")
}

func (sb *Sealer) ReleaseReplicaUpgrade(ctx context.Context, sector storage.SectorRef) error {
	return xerrors.Errorf("not supported at this layer")
}

func (sb *Sealer) ReleaseSectorKey(ctx context.Context, sector storage.SectorRef) error {
	return xerrors.Errorf("not supported at this layer")
}

func (sb *Sealer) Remove(ctx context.Context, sector storage.SectorRef) error {
	return xerrors.Errorf("not supported at this layer") // happens in localworker
}

func GetRequiredPadding(oldLength abi.PaddedPieceSize, newPieceLength abi.PaddedPieceSize) ([]abi.PaddedPieceSize, abi.PaddedPieceSize) {

	padPieces := make([]abi.PaddedPieceSize, 0)

	toFill := uint64(-oldLength % newPieceLength)

	n := bits.OnesCount64(toFill)
	var sum abi.PaddedPieceSize
	for i := 0; i < n; i++ {
		next := bits.TrailingZeros64(toFill)
		psize := uint64(1) << uint(next)
		toFill ^= psize

		padded := abi.PaddedPieceSize(psize)
		padPieces = append(padPieces, padded)
		sum += padded
	}

	return padPieces, sum
}

func GenerateUnsealedCID(proofType abi.RegisteredSealProof, pieces []abi.PieceInfo) (cid.Cid, error) {
	ssize, err := proofType.SectorSize()
	if err != nil {
		return cid.Undef, err
	}

	pssize := abi.PaddedPieceSize(ssize)
	allPieces := make([]abi.PieceInfo, 0, len(pieces))
	if len(pieces) == 0 {
		allPieces = append(allPieces, abi.PieceInfo{
			Size:     pssize,
			PieceCID: zerocomm.ZeroPieceCommitment(pssize.Unpadded()),
		})
	} else {
		var sum abi.PaddedPieceSize

		padTo := func(pads []abi.PaddedPieceSize) {
			for _, p := range pads {
				allPieces = append(allPieces, abi.PieceInfo{
					Size:     p,
					PieceCID: zerocomm.ZeroPieceCommitment(p.Unpadded()),
				})

				sum += p
			}
		}

		for _, p := range pieces {
			ps, _ := GetRequiredPadding(sum, p.Size)
			padTo(ps)

			allPieces = append(allPieces, p)
			sum += p.Size
		}

		ps, _ := GetRequiredPadding(sum, pssize)
		padTo(ps)
	}

	return ffi.GenerateUnsealedCID(proofType, allPieces)
}

//拷贝文件
func CopyFile(src, dst string) bool {
	if len(src) == 0 || len(dst) == 0 {
		return false
	}
	srcFile, e := os.OpenFile(src, os.O_RDONLY, os.ModePerm)
	if e != nil {
		log.Error("copyfile", e)
		return false
	}
	defer srcFile.Close()

	dst = strings.Replace(dst, "\\", "/", -1)
	dstPathArr := strings.Split(dst, "/")
	dstPathArr = dstPathArr[0 : len(dstPathArr)-1]
	dstPath := strings.Join(dstPathArr, "/")

	dstFileInfo := GetFileInfo(dstPath)
	if dstFileInfo == nil {
		if e := os.MkdirAll(dstPath, os.ModePerm); e != nil {
			log.Error("copyfile", e)
			return false
		}
	}
	//这里要把O_TRUNC 加上，否则会出现新旧文件内容出现重叠现象
	dstFile, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_RDWR, os.ModePerm)
	if e != nil {
		log.Error("copyfile", e)
		return false
	}
	defer dstFile.Close()
	//fileInfo, e := srcFile.Stat()
	//fileInfo.Size() > 1024
	//byteBuffer := make([]byte, 10)
	if _, e := io.Copy(dstFile, srcFile); e != nil {
		log.Error("copyfile", e)
		return false
	} else {
		return true
	}

}

func GetFileInfo(src string) os.FileInfo {
	if fileInfo, e := os.Stat(src); e != nil {
		if os.IsNotExist(e) {
			return nil
		}
		return nil
	} else {
		return fileInfo
	}
}

func WriteSectorCC(path string, data []byte) error {
	//先判断是否存在
	isExist, err := PathExists(path)
	if err != nil {
		log.Error("WriteSectorCC PathExists Error : >.>", err)
	}
	if isExist {
		return nil
	}
	err = ioutil.WriteFile(path, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

func ReadSecorCC(path string) (cid.Cid, error) {
	input, err := ioutil.ReadFile(path)
	if err != nil {
		return cid.Cid{}, err
	}
	var c cid.Cid
	err = c.UnmarshalJSON(input)
	if err != nil {
		return cid.Cid{}, err
	}
	return c, nil
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
