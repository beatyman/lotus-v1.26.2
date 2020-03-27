package api

import (
	"bytes"
	"context"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/go-sectorbuilder/database"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/crypto"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sectorstorage/stores"
)

// alias because cbor-gen doesn't like non-alias types
type SectorState = uint64

const (
	UndefinedSectorState SectorState = iota

	// happy path
	Empty
	Packing // sector not in sealStore, and not on chain

	Unsealed      // sealing / queued
	PreCommitting // on chain pre-commit
	WaitSeed      // waiting for seed
	Committing
	CommitWait // waiting for message to land on chain
	FinalizeSector
	Proving
	_ // reserved
	_
	_

	// recovery handling
	// Reseal
	_
	_
	_
	_
	_
	_
	_

	// error modes
	FailedUnrecoverable

	SealFailed
	PreCommitFailed
	SealCommitFailed
	CommitFailed
	PackingFailed
	_
	_
	_

	Faulty        // sector is corrupted or gone for some reason
	FaultReported // sector has been declared as a fault on chain
	FaultedFinal  // fault declared on chain
)

var SectorStates = []string{
	UndefinedSectorState: "UndefinedSectorState",
	Empty:                "Empty",
	Packing:              "Packing",
	Unsealed:             "Unsealed",
	PreCommitting:        "PreCommitting",
	WaitSeed:             "WaitSeed",
	Committing:           "Committing",
	CommitWait:           "CommitWait",
	FinalizeSector:       "FinalizeSector",
	Proving:              "Proving",

	SealFailed:       "SealFailed",
	PreCommitFailed:  "PreCommitFailed",
	SealCommitFailed: "SealCommitFailed",
	CommitFailed:     "CommitFailed",
	PackingFailed:    "PackingFailed",

	FailedUnrecoverable: "FailedUnrecoverable",

	Faulty:        "Faulty",
	FaultReported: "FaultReported",
	FaultedFinal:  "FaultedFinal",
}

// StorageMiner is a low-level interface to the Filecoin network storage miner node
type StorageMiner interface {
	Common

	ActorAddress(context.Context) (address.Address, error)

	ActorSectorSize(context.Context, address.Address) (abi.SectorSize, error)

	// Temp api for testing
	PledgeSector(context.Context) error

	// Get the status of a given sector by ID
	SectorsStatus(context.Context, abi.SectorNumber) (SectorInfo, error)

	// List all staged sectors
	SectorsList(context.Context) ([]abi.SectorNumber, error)

	SectorsRefs(context.Context) (map[string][]SealedRef, error)

	SectorsUpdate(context.Context, abi.SectorNumber, SectorState) error

	StorageList(ctx context.Context) (map[stores.ID][]stores.Decl, error)
	StorageLocal(ctx context.Context) (map[stores.ID]string, error)
	StorageStat(ctx context.Context, id stores.ID) (stores.FsStat, error)

	// WorkerConnect tells the node to connect to workers RPC
	WorkerConnect(context.Context, string) error
	WorkerStats(context.Context) (map[uint64]WorkerStats, error)

	stores.SectorIndex

	MarketImportDealData(ctx context.Context, propcid cid.Cid, path string) error
	MarketListDeals(ctx context.Context) ([]storagemarket.StorageDeal, error)
	MarketListIncompleteDeals(ctx context.Context) ([]storagemarket.MinerDeal, error)
	MarketSetPrice(context.Context, types.BigInt) error

	DealsImportData(ctx context.Context, dealPropCid cid.Cid, file string) error
	DealsList(ctx context.Context) ([]storagemarket.StorageDeal, error)

	StorageAddLocal(ctx context.Context, path string) error

	// implements by hlm
	RunPledgeSector(context.Context) error
	StatusPledgeSector(context.Context) (int, error)
	StopPledgeSector(context.Context) error

	// Message communication
	MpoolPushMessage(context.Context, *types.Message) (*types.SignedMessage, error) // get nonce, sign, push
	StateWaitMsg(context.Context, cid.Cid) (*MsgLookup, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)

	SectorsListAll(context.Context) ([]SectorInfo, error)
	WorkerAddress(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	WorkerStatsAll(ctx context.Context) ([]sectorbuilder.WorkerRemoteStats, error)
	WorkerQueue(context.Context, sectorbuilder.WorkerCfg) (<-chan sectorbuilder.WorkerTask, error)
	WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error)
	WorkerPushing(ctx context.Context, taskKey string) error
	WorkerDone(ctx context.Context, res sectorbuilder.SealRes) error
	WorkerDisable(ctx context.Context, wid string, disable bool) error

	//Storage
	AddStorage(ctx context.Context, sInfo database.StorageInfo) error
	DisableStorage(ctx context.Context, id int64) error
	MountStorage(ctx context.Context, id int64) error
	UMountStorage(ctx context.Context, id int64) error
	RelinkStorage(ctx context.Context, id int64) error
	ScaleStorage(ctx context.Context, id int64, size int64, work int64) error
}

type SealRes struct {
	Err   string
	GoErr error `json:"-"`

	Proof []byte
}

type SectorLog struct {
	Kind      string
	Timestamp uint64

	Trace string

	Message string
}

type SectorInfo struct {
	SectorID abi.SectorNumber
	State    SectorState
	CommD    *cid.Cid
	CommR    *cid.Cid
	Proof    []byte
	Deals    []abi.DealID
	Ticket   SealTicket
	Seed     SealSeed
	Retries  uint64

	LastErr string

	Log []SectorLog
}

type SealedRef struct {
	SectorID abi.SectorNumber
	Offset   uint64
	Size     abi.UnpaddedPieceSize
}

type SealedRefs struct {
	Refs []SealedRef
}

type SealTicket struct {
	Value abi.SealRandomness
	Epoch abi.ChainEpoch
}

type SealSeed struct {
	Value abi.InteractiveSealRandomness
	Epoch abi.ChainEpoch
}

func (st *SealTicket) Equals(ost *SealTicket) bool {
	return bytes.Equal(st.Value, ost.Value) && st.Epoch == ost.Epoch
}

func (st *SealSeed) Equals(ost *SealSeed) bool {
	return bytes.Equal(st.Value, ost.Value) && st.Epoch == ost.Epoch
}
