//+build cgo

package ffiwrapper

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/filecoin-project/specs-actors/actors/runtime/proof"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	ffi "github.com/filecoin-project/filecoin-ffi"

	"github.com/filecoin-project/lotus/extern/sector-storage/stores"

	"github.com/gwaylib/errors"
	"go.opencensus.io/trace"
)

func (sb *Sealer) generateWinningPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []proof.SectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, error) {
	randomness[31] &= 0x3f
	privsectors, skipped, done, err := sb.pubSectorToPriv(ctx, minerID, sectorInfo, nil, abi.RegisteredSealProof.RegisteredWinningPoStProof) // TODO: FAULTS?
	if err != nil {
		return nil, err
	}
	defer done()
	if len(skipped) > 0 {
		return nil, xerrors.Errorf("pubSectorToPriv skipped sectors: %+v", skipped)
	}

	return ffi.GenerateWinningPoSt(minerID, privsectors, randomness)
}

// 扩展[]abi.PoStProof约定, 以便兼容官方接口
// PoStProof {
//    PoStProof RegisteredPoStProof
//    ProofBytes []byte
// }
// 其中的PoStProof.PoStProof为类型，>=0时使用官方原值, 小于0时由我方自定义
// PoStProof.PoStProof == -100 时，为扩展标识，PoStProof.ProofBytes为json字符串序列化出来的字节，内容定义如下：
//    {
//        "code":"1001", // 0，成功(未启用)；1001,扇区证明失败；1002, 扇区文件读取超时; 1003，扇区文件错误
//        "msg":"", // 需要传递的消息，错误时为错误信息
//        "sid":1000, //扇区编号值
//    }
//
type WindowPoStErrResp struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Sid  int64  `json:"sid"`
}

func (sb *Sealer) generateWindowPoSt(ctx context.Context, minerID abi.ActorID, sectorInfo []proof.SectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, []abi.SectorID, error) {
	randomness[31] &= 0x3f
	privsectors, skipped, done, err := sb.pubSectorToPriv(ctx, minerID, sectorInfo, nil, abi.RegisteredSealProof.RegisteredWindowPoStProof)
	if err != nil {
		return nil, nil, xerrors.Errorf("gathering sector info: %w", err)
	}
	defer done()

	proofs, err := ffi.GenerateWindowPoSt(minerID, privsectors, randomness)
	if err != nil {
		return proofs, skipped, err
	}
	sucProof := []proof.PoStProof{}
	for _, p := range proofs {
		if p.PoStProof >= 0 {
			sucProof = append(sucProof, p)
			continue
		}
		switch p.PoStProof {
		case -100:
			resp := WindowPoStErrResp{}
			if err := json.Unmarshal(p.ProofBytes, &resp); err != nil {
				return proofs, skipped, errors.As(err, "Unexpecte protocal", string(p.ProofBytes))
			}
			skipped = append(skipped, abi.SectorID{
				Miner:  minerID,
				Number: abi.SectorNumber(resp.Sid),
			})
			// TODO:标记并处理错误的扇区
		}
	}
	return sucProof, skipped, err
}

func (sb *Sealer) pubSectorToPriv(ctx context.Context, mid abi.ActorID, sectorInfo []proof.SectorInfo, faults []abi.SectorNumber, rpt func(abi.RegisteredSealProof) (abi.RegisteredPoStProof, error)) (ffi.SortedPrivateSectorInfo, []abi.SectorID, func(), error) {
	sectors := []abi.SectorID{}
	for _, s := range sectorInfo {
		sectors = append(sectors, abi.SectorID{Miner: mid, Number: s.SectorNumber})
	}
	_, skipped, err := CheckProvable(sb.sectors.RepoPath(), sb.ssize, sectors, 3*time.Second)
	if err != nil {
		return ffi.SortedPrivateSectorInfo{}, nil, nil, errors.As(err)
	}

	fmap := map[abi.SectorNumber]struct{}{}
	for _, fault := range skipped {
		fmap[fault.Number] = struct{}{}
	}

	var doneFuncs []func()
	done := func() {
		for _, df := range doneFuncs {
			df()
		}
	}

	var out []ffi.PrivateSectorInfo
	for _, s := range sectorInfo {
		if _, faulty := fmap[s.SectorNumber]; faulty {
			continue
		}

		sid := abi.SectorID{Miner: mid, Number: s.SectorNumber}

		// Ignore to visit the storage. by hlm
		//paths, d, err := sb.sectors.AcquireSector(ctx, sid, stores.FTCache|stores.FTSealed, 0, stores.PathStorage)
		//if err != nil {
		//	log.Warnw("failed to acquire sector, skipping", "sector", sid, "error", err)
		//	skipped = append(skipped, sid)
		//	continue
		//}
		//doneFuncs = append(doneFuncs, d)
		repo := sb.sectors.RepoPath()
		sName := SectorName(sid)
		paths := stores.SectorPaths{
			ID:       sid,
			Unsealed: filepath.Join(repo, "unsealed", sName),
			Sealed:   filepath.Join(repo, "sealed", sName),
			Cache:    filepath.Join(repo, "cache", sName),
		}

		postProofType, err := rpt(s.SealProof)
		if err != nil {
			done()
			return ffi.SortedPrivateSectorInfo{}, nil, nil, xerrors.Errorf("acquiring registered PoSt proof from sector info %+v: %w", s, err)
		}

		out = append(out, ffi.PrivateSectorInfo{
			CacheDirPath:     paths.Cache,
			PoStProofType:    postProofType,
			SealedSectorPath: paths.Sealed,
			SectorInfo:       s,
		})
	}

	return ffi.NewSortedPrivateSectorInfo(out...), skipped, done, nil
}

var _ Verifier = ProofVerifier

type proofVerifier struct{}

var ProofVerifier = proofVerifier{}

func (proofVerifier) VerifySeal(info proof.SealVerifyInfo) (bool, error) {
	return ffi.VerifySeal(info)
}

func (proofVerifier) VerifyWinningPoSt(ctx context.Context, info proof.WinningPoStVerifyInfo) (bool, error) {
	info.Randomness[31] &= 0x3f
	_, span := trace.StartSpan(ctx, "VerifyWinningPoSt")
	defer span.End()

	return ffi.VerifyWinningPoSt(info)
}

func (proofVerifier) VerifyWindowPoSt(ctx context.Context, info proof.WindowPoStVerifyInfo) (bool, error) {
	info.Randomness[31] &= 0x3f
	_, span := trace.StartSpan(ctx, "VerifyWindowPoSt")
	defer span.End()

	return ffi.VerifyWindowPoSt(info)
}

func (proofVerifier) GenerateWinningPoStSectorChallenge(ctx context.Context, proofType abi.RegisteredPoStProof, minerID abi.ActorID, randomness abi.PoStRandomness, eligibleSectorCount uint64) ([]uint64, error) {
	randomness[31] &= 0x3f
	return ffi.GenerateWinningPoStSectorChallenge(proofType, minerID, randomness, eligibleSectorCount)
}
