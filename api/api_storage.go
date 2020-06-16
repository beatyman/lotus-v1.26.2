package api

import (
	"bytes"
	"context"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/sector-storage/database"
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/sector-storage/stores"
	"github.com/filecoin-project/sector-storage/storiface"
	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/chain/types"
)

// StorageMiner is a low-level interface to the Filecoin network storage miner node
type StorageMiner interface {
	Common

	ActorAddress(context.Context) (address.Address, error)

	ActorSectorSize(context.Context, address.Address) (abi.SectorSize, error)

	MiningBase(context.Context) (*types.TipSet, error)

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
	WorkerStats(context.Context) (map[uint64]storiface.WorkerStats, error)

	stores.SectorIndex

	MarketImportDealData(ctx context.Context, propcid cid.Cid, path string) error
	MarketListDeals(ctx context.Context) ([]storagemarket.StorageDeal, error)
	MarketListIncompleteDeals(ctx context.Context) ([]storagemarket.MinerDeal, error)
	MarketSetPrice(context.Context, types.BigInt) error

	DealsImportData(ctx context.Context, dealPropCid cid.Cid, file string) error
	DealsList(ctx context.Context) ([]storagemarket.StorageDeal, error)
	DealsSetAcceptingStorageDeals(context.Context, bool) error

	StorageAddLocal(ctx context.Context, path string) error

	// implements by hlm
	RunPledgeSector(context.Context) error
	StatusPledgeSector(context.Context) (int, error)
	StopPledgeSector(context.Context) error

	SectorsListAll(context.Context) ([]SectorInfo, error)
	WorkerAddress(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	WorkerStatus(ctx context.Context) (ffiwrapper.WorkerStats, error)
	WorkerStatusAll(ctx context.Context) ([]ffiwrapper.WorkerRemoteStats, error)
	WorkerQueue(context.Context, ffiwrapper.WorkerCfg) (<-chan ffiwrapper.WorkerTask, error)
	WorkerWorking(ctx context.Context, workerId string) (database.WorkingSectors, error)
	WorkerLock(ctx context.Context, workerId, taskKey, memo string, status int) error
	WorkerUnlock(ctx context.Context, workerId, taskKey, memo string) error
	WorkerDone(ctx context.Context, res ffiwrapper.SealRes) error
	WorkerDisable(ctx context.Context, wid string, disable bool) error
	WorkerAddConn(ctx context.Context, wid string, num int) error
	WorkerPreConn(ctx context.Context) (*database.WorkerInfo, error)
	WorkerMinerConn(ctx context.Context) (int, error)

	//Storage
	AddHLMStorage(ctx context.Context, sInfo database.StorageInfo) error
	DisableHLMStorage(ctx context.Context, id int64) error
	MountHLMStorage(ctx context.Context, id int64) error
	UMountHLMStorage(ctx context.Context, id int64) error
	RelinkHLMStorage(ctx context.Context, id int64) error
	ScaleHLMStorage(ctx context.Context, id int64, size int64, work int64) error
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

type SectorState string
