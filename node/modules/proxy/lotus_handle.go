package proxy

import (
	"context"
	"encoding/json"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/builtin/v8/paych"
	"github.com/filecoin-project/go-state-types/builtin/v9/miner"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/dline"
	abinetwork "github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/api"
	lminer "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/filecoin-project/lotus/journal/alerting"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo/imports"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	blocks "github.com/ipfs/go-block-format"
	metrics "github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/network"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	"golang.org/x/xerrors"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	datatransfer "github.com/filecoin-project/go-data-transfer/v2"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	apitypes "github.com/filecoin-project/lotus/api/types"
	"github.com/filecoin-project/lotus/chain/types"
)

// TODO: rebuild the go-jsonrpc to make proxy
type LotusImpl struct {
	secret *dtypes.APIAlg
	token  string
}

func (s *LotusImpl) ChainExportRangeInternal(ctx context.Context, p0, p1 types.TipSetKey, p2 api.ChainExportConfig) error {
	return bestNodeApi().ChainExportRangeInternal(ctx,p0,p1,p2)
}

func (s *LotusImpl) ChainHotGC(ctx context.Context, p0 api.HotGCOpts) error {
	return bestNodeApi().ChainHotGC(ctx,p0)
}

func (s *LotusImpl) EthGetTransactionByHashLimited(ctx context.Context, p0 *ethtypes.EthHash, p1 abi.ChainEpoch) (*ethtypes.EthTx, error) {
	return bestNodeApi().EthGetTransactionByHashLimited(ctx,p0,p1)
}

func (s *LotusImpl) EthGetTransactionReceiptLimited(ctx context.Context, p0 ethtypes.EthHash, p1 abi.ChainEpoch) (*api.EthTxReceipt, error) {
	return bestNodeApi().EthGetTransactionReceiptLimited(ctx,p0,p1)
}

func (s *LotusImpl) StartTime(ctx context.Context) (time.Time, error) {
	return bestNodeApi().StartTime(ctx)
}

func (s *LotusImpl) RaftState(ctx context.Context) (*api.RaftStateData, error) {
	return bestNodeApi().RaftState(ctx)
}

func (s *LotusImpl) RaftLeader(ctx context.Context) (peer.ID, error) {
	return bestNodeApi().RaftLeader(ctx)
}

func (s *LotusImpl) StateGetAllocationForPendingDeal(p0 context.Context, p1 abi.DealID, p2 types.TipSetKey) (*verifreg.Allocation, error) {
	return bestNodeApi().StateGetAllocationForPendingDeal(p0, p1,p2)
}

func (s *LotusImpl) StateGetAllocation(p0 context.Context, p1 address.Address, p2 verifreg.AllocationId, p3 types.TipSetKey) (*verifreg.Allocation, error) {
	return bestNodeApi().StateGetAllocation(p0, p1,p2,p3)
}

func (s *LotusImpl) StateGetAllocations(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (map[verifreg.AllocationId]verifreg.Allocation, error) {
	return bestNodeApi().StateGetAllocations(p0, p1,p2)
}

func (s *LotusImpl) StateGetClaim(p0 context.Context, p1 address.Address, p2 verifreg.ClaimId, p3 types.TipSetKey) (*verifreg.Claim, error) {
	return bestNodeApi().StateGetClaim(p0, p1,p2,p3)
}

func (s *LotusImpl) StateGetClaims(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (map[verifreg.ClaimId]verifreg.Claim, error) {
	return bestNodeApi().StateGetClaims(p0, p1,p2)
}

func NewLotusProxy(token string) api.FullNode {
	impl := &LotusImpl{
		token: token,
	}
	return impl
}

type jwtPayload struct {
	Allow []auth.Permission
}

func (s *LotusImpl ) FilecoinAddressToEthAddress(ctx context.Context, p1 address.Address) (ethtypes.EthAddress, error)  {
	return bestNodeApi().FilecoinAddressToEthAddress(ctx,p1)
}
func (s *LotusImpl) EthAddressToFilecoinAddress(ctx context.Context, p1 ethtypes.EthAddress) (address.Address, error){
	return bestNodeApi().EthAddressToFilecoinAddress(ctx,p1)
}
func (s *LotusImpl) ChainGetEvents(ctx context.Context, p1 cid.Cid) ([]types.Event, error){
	return bestNodeApi().ChainGetEvents(ctx,p1)
}
func (s *LotusImpl) EthAccounts(ctx context.Context) ([]ethtypes.EthAddress, error) {
	return bestNodeApi().EthAccounts(ctx)
}

func (s *LotusImpl) EthBlockNumber(ctx context.Context) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthBlockNumber(ctx)
}

func (s *LotusImpl) EthGetBlockTransactionCountByNumber(ctx context.Context, p1 ethtypes.EthUint64) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthGetBlockTransactionCountByNumber(ctx,p1)
}

func (s *LotusImpl) EthGetBlockTransactionCountByHash(ctx context.Context, p1 ethtypes.EthHash) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthGetBlockTransactionCountByHash(ctx,p1)
}

func (s *LotusImpl) EthGetBlockByHash(ctx context.Context, p1 ethtypes.EthHash, p2 bool) (ethtypes.EthBlock, error) {
	return bestNodeApi().EthGetBlockByHash(ctx,p1,p2)
}

func (s *LotusImpl) EthGetBlockByNumber(ctx context.Context, p1 string, p2 bool) (ethtypes.EthBlock, error) {
	return bestNodeApi().EthGetBlockByNumber(ctx,p1,p2)
}

func (s *LotusImpl) EthGetTransactionByHash(ctx context.Context, p1 *ethtypes.EthHash) (*ethtypes.EthTx, error) {
	return bestNodeApi().EthGetTransactionByHash(ctx,p1)
}

func (s *LotusImpl) EthGetTransactionHashByCid(ctx context.Context, p1 cid.Cid) (*ethtypes.EthHash, error)      {
	return bestNodeApi().EthGetTransactionHashByCid(ctx,p1)
}

func (s *LotusImpl) EthGetMessageCidByTransactionHash(ctx context.Context, p1 *ethtypes.EthHash) (*cid.Cid, error)       {
	return bestNodeApi().EthGetMessageCidByTransactionHash(ctx,p1)
}

func (s *LotusImpl) EthGetTransactionCount(ctx context.Context, p1 ethtypes.EthAddress, p2 string) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthGetTransactionCount(ctx,p1,p2)
}

func (s *LotusImpl) EthGetTransactionReceipt(ctx context.Context, p1 ethtypes.EthHash) (*api.EthTxReceipt, error) {
	return bestNodeApi().EthGetTransactionReceipt(ctx,p1)
}

func (s *LotusImpl) EthGetTransactionByBlockHashAndIndex(ctx context.Context, p1 ethtypes.EthHash, p2 ethtypes.EthUint64) (ethtypes.EthTx, error) {
	return bestNodeApi().EthGetTransactionByBlockHashAndIndex(ctx,p1,p2)
}

func (s *LotusImpl) EthGetTransactionByBlockNumberAndIndex(ctx context.Context, p1 ethtypes.EthUint64, p2 ethtypes.EthUint64) (ethtypes.EthTx, error) {
	return bestNodeApi().EthGetTransactionByBlockNumberAndIndex(ctx,p1,p2)
}

func (s *LotusImpl) EthGetCode(ctx context.Context, p1 ethtypes.EthAddress, p2 string) (ethtypes.EthBytes, error) {
	return bestNodeApi().EthGetCode(ctx,p1,p2)
}

func (s *LotusImpl) EthGetStorageAt(ctx context.Context, p1 ethtypes.EthAddress, p2 ethtypes.EthBytes, p3 string) (ethtypes.EthBytes, error) {
	return bestNodeApi().EthGetStorageAt(ctx,p1,p2,p3)
}

func (s *LotusImpl) EthGetBalance(ctx context.Context, p1 ethtypes.EthAddress, p2 string) (ethtypes.EthBigInt, error) {
	return bestNodeApi().EthGetBalance(ctx,p1,p2)
}

func (s *LotusImpl) EthChainId(ctx context.Context) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthChainId(ctx)
}

func (s *LotusImpl) NetVersion(ctx context.Context) (string, error) {
	return bestNodeApi().NetVersion(ctx)
}

func (s *LotusImpl) NetListening(ctx context.Context) (bool, error) {
	return bestNodeApi().NetListening(ctx)
}

func (s *LotusImpl) EthProtocolVersion(ctx context.Context) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthProtocolVersion(ctx)
}

func (s *LotusImpl) EthGasPrice(ctx context.Context) (ethtypes.EthBigInt, error) {
	return bestNodeApi().EthGasPrice(ctx)
}

func (s *LotusImpl) 	EthFeeHistory(ctx context.Context, p1 jsonrpc.RawParams) (ethtypes.EthFeeHistory, error)    {
	return bestNodeApi().EthFeeHistory(ctx,p1)
}

func (s *LotusImpl) EthMaxPriorityFeePerGas(ctx context.Context) (ethtypes.EthBigInt, error) {
	return bestNodeApi().EthMaxPriorityFeePerGas(ctx)
}

func (s *LotusImpl) EthEstimateGas(ctx context.Context, p1 ethtypes.EthCall) (ethtypes.EthUint64, error) {
	return bestNodeApi().EthEstimateGas(ctx,p1)
}

func (s *LotusImpl) EthCall(ctx context.Context, p1 ethtypes.EthCall, p2 string) (ethtypes.EthBytes, error) {
	return bestNodeApi().EthCall(ctx,p1,p2)
}

func (s *LotusImpl) EthSendRawTransaction(ctx context.Context, p1 ethtypes.EthBytes) (ethtypes.EthHash, error) {
	return bestNodeApi().EthSendRawTransaction(ctx,p1)
}

func (s *LotusImpl) EthGetLogs(ctx context.Context, p1 *ethtypes.EthFilterSpec) (*ethtypes.EthFilterResult, error) {
	return bestNodeApi().EthGetLogs(ctx,p1)
}

func (s *LotusImpl) EthGetFilterChanges(ctx context.Context, p1 ethtypes.EthFilterID) (*ethtypes.EthFilterResult, error) {
	return bestNodeApi().EthGetFilterChanges(ctx,p1)
}

func (s *LotusImpl) EthGetFilterLogs(ctx context.Context, p1 ethtypes.EthFilterID) (*ethtypes.EthFilterResult, error) {
	return bestNodeApi().EthGetFilterLogs(ctx,p1)
}

func (s *LotusImpl) EthNewFilter(ctx context.Context, p1 *ethtypes.EthFilterSpec) (ethtypes.EthFilterID, error) {
	return bestNodeApi().EthNewFilter(ctx,p1)
}

func (s *LotusImpl) EthNewBlockFilter(ctx context.Context) (ethtypes.EthFilterID, error) {
	return bestNodeApi().EthNewBlockFilter(ctx)
}

func (s *LotusImpl) EthNewPendingTransactionFilter(ctx context.Context) (ethtypes.EthFilterID, error) {
	return bestNodeApi().EthNewPendingTransactionFilter(ctx)
}

func (s *LotusImpl) EthUninstallFilter(ctx context.Context, p1 ethtypes.EthFilterID) (bool, error) {
	return bestNodeApi().EthUninstallFilter(ctx,p1)
}

func (s *LotusImpl) EthSubscribe(ctx context.Context, p1 jsonrpc.RawParams) (ethtypes.EthSubscriptionID, error) {
	return bestNodeApi().EthSubscribe(ctx,p1)
}

func (s *LotusImpl) EthUnsubscribe(ctx context.Context, p1 ethtypes.EthSubscriptionID) (bool, error) {
	return bestNodeApi().EthUnsubscribe(ctx,p1)
}

func (s *LotusImpl) Web3ClientVersion(ctx context.Context) (string, error) {
	return bestNodeApi().Web3ClientVersion(ctx)
}
func (s *LotusImpl)NetPing (ctx context.Context, p1 peer.ID) (time.Duration, error){
	return bestNodeApi().NetPing(ctx, p1)
}
func (s *LotusImpl) NetProtectAdd(ctx context.Context, acl []peer.ID) error {
	return bestNodeApi().NetProtectAdd(ctx, acl)
}

func (s *LotusImpl) NetProtectRemove(ctx context.Context, acl []peer.ID) error {
	return bestNodeApi().NetProtectRemove(ctx, acl)
}

func (s *LotusImpl) NetProtectList(ctx context.Context) ([]peer.ID, error) {
	return bestNodeApi().NetProtectList(ctx)
}

func (s *LotusImpl) NetStat(ctx context.Context, scope string) (api.NetStat, error) {
	return bestNodeApi().NetStat(ctx, scope)
}

func (s *LotusImpl) NetLimit(ctx context.Context, scope string) (api.NetLimit, error) {
	return bestNodeApi().NetLimit(ctx, scope)
}

func (s *LotusImpl) NetSetLimit(ctx context.Context, scope string, limit api.NetLimit) error {
	return bestNodeApi().NetSetLimit(ctx, scope, limit)
}

func (s *LotusImpl) PaychFund(ctx context.Context, from, to address.Address, amt types.BigInt) (*api.ChannelInfo, error) {
	return bestNodeApi().PaychFund(ctx, from, to, amt)
}
func (s *LotusImpl) PaychGet(p0 context.Context, p1 address.Address, p2 address.Address, p3 types.BigInt, p4 api.PaychGetOpts) (*api.ChannelInfo, error) {
	return bestNodeApi().PaychGet(p0, p1, p2, p3, p4)
}

// rebuild by zhoushuyue
func (s *LotusImpl) LogAlerts(p0 context.Context) ([]alerting.Alert, error) {
	return bestNodeApi().LogAlerts(p0)
}
func (s *LotusImpl) Closing(p0 context.Context) (<-chan struct{}, error) {
	//return bestNodeApi().Closing(p0)
	return closingAll(p0)
}
func (l *LotusImpl) AuthVerify(ctx context.Context, token string) ([]auth.Permission, error) {
	if token == l.token {
		return api.AllPermissions, nil
	}
	return nil, xerrors.New("JWT Verification not match")

	// TODO: jwt auth
	var payload jwtPayload
	if _, err := jwt.Verify([]byte(token), (*jwt.HMACSHA)(l.secret), &payload); err != nil {
		return nil, xerrors.Errorf("JWT Verification failed: %w", err)
	}

	return payload.Allow, nil
}

func (s *LotusImpl) MpoolPush(p0 context.Context, p1 *types.SignedMessage) (cid.Cid, error) {
	//return bestNodeApi().MpoolPush(p0, p1)
	cid := p1.Cid()
	err := broadcastSignedMessage(p0, p1)
	return cid, err
}

func (s *LotusImpl) MpoolPushMessage(p0 context.Context, p1 *types.Message, p2 *api.MessageSendSpec) (*types.SignedMessage, error) {
	//return bestNodeApi().MpoolPushMessage(p0, p1, p2)
	smsg, err := broadcastMessage(p0, p1, p2)
	return smsg, err
}

func (s *LotusImpl) StateWaitMsg(p0 context.Context, p1 cid.Cid, p2 uint64, p3 abi.ChainEpoch, p4 bool) (*api.MsgLookup, error) {
	//return bestNodeApi().StateWaitMsg(p0, p1, p2, p3, p4)
	return multiStateWaitMsg(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) ChainNotify(p0 context.Context) (<-chan []*api.HeadChange, error) {
	return bestNodeApi().ChainNotify(p0)
	//return chainNotify(p0) // TODO: implement the chan, but we not use this yet. zhoushyue
}

// rebuild end

func (s *LotusImpl) AuthNew(p0 context.Context, p1 []auth.Permission) ([]byte, error) {
	return bestNodeApi().AuthNew(p0, p1)
}

func (s *LotusImpl) Discover(p0 context.Context) (apitypes.OpenRPCDocument, error) {
	return bestNodeApi().Discover(p0)
}

func (s *LotusImpl) ID(p0 context.Context) (peer.ID, error) {
	return bestNodeApi().ID(p0)
}

func (s *LotusImpl) LogList(p0 context.Context) ([]string, error) {
	return bestNodeApi().LogList(p0)
}

func (s *LotusImpl) LogSetLevel(p0 context.Context, p1 string, p2 string) error {
	return bestNodeApi().LogSetLevel(p0, p1, p2)
}

func (s *LotusImpl) NetAddrsListen(p0 context.Context) (peer.AddrInfo, error) {
	return bestNodeApi().NetAddrsListen(p0)
}

func (s *LotusImpl) NetAgentVersion(p0 context.Context, p1 peer.ID) (string, error) {
	return bestNodeApi().NetAgentVersion(p0, p1)
}

func (s *LotusImpl) NetAutoNatStatus(p0 context.Context) (api.NatInfo, error) {
	return bestNodeApi().NetAutoNatStatus(p0)
}

func (s *LotusImpl) NetBandwidthStats(p0 context.Context) (metrics.Stats, error) {
	return bestNodeApi().NetBandwidthStats(p0)
}

func (s *LotusImpl) NetBandwidthStatsByPeer(p0 context.Context) (map[string]metrics.Stats, error) {
	return bestNodeApi().NetBandwidthStatsByPeer(p0)
}

func (s *LotusImpl) NetBandwidthStatsByProtocol(p0 context.Context) (map[protocol.ID]metrics.Stats, error) {
	return bestNodeApi().NetBandwidthStatsByProtocol(p0)
}

func (s *LotusImpl) NetBlockAdd(p0 context.Context, p1 api.NetBlockList) error {
	return bestNodeApi().NetBlockAdd(p0, p1)
}

func (s *LotusImpl) NetBlockList(p0 context.Context) (api.NetBlockList, error) {
	return bestNodeApi().NetBlockList(p0)
}

func (s *LotusImpl) NetBlockRemove(p0 context.Context, p1 api.NetBlockList) error {
	return bestNodeApi().NetBlockRemove(p0, p1)
}

func (s *LotusImpl) NetConnect(p0 context.Context, p1 peer.AddrInfo) error {
	return bestNodeApi().NetConnect(p0, p1)
}

func (s *LotusImpl) NetConnectedness(p0 context.Context, p1 peer.ID) (network.Connectedness, error) {
	return bestNodeApi().NetConnectedness(p0, p1)
}

func (s *LotusImpl) NetDisconnect(p0 context.Context, p1 peer.ID) error {
	return bestNodeApi().NetDisconnect(p0, p1)
}

func (s *LotusImpl) NetFindPeer(p0 context.Context, p1 peer.ID) (peer.AddrInfo, error) {
	return bestNodeApi().NetFindPeer(p0, p1)
}

func (s *LotusImpl) NetPeerInfo(p0 context.Context, p1 peer.ID) (*api.ExtendedPeerInfo, error) {
	return bestNodeApi().NetPeerInfo(p0, p1)
}

func (s *LotusImpl) NetPeers(p0 context.Context) ([]peer.AddrInfo, error) {
	return bestNodeApi().NetPeers(p0)
}

func (s *LotusImpl) NetPubsubScores(p0 context.Context) ([]api.PubsubScore, error) {
	return bestNodeApi().NetPubsubScores(p0)
}

func (s *LotusImpl) Session(p0 context.Context) (uuid.UUID, error) {
	return bestNodeApi().Session(p0)
}

func (s *LotusImpl) Shutdown(p0 context.Context) error {
	return bestNodeApi().Shutdown(p0)
}

func (s *LotusImpl) Version(p0 context.Context) (api.APIVersion, error) {
	return bestNodeApi().Version(p0)
}

func (s *LotusImpl) MpoolSignMessage(p0 context.Context, p1 *types.Message, p2 *api.MessageSendSpec) (*types.SignedMessage, error) {
	return bestNodeApi().MpoolSignMessage(p0, p1, p2)
}
func (c *LotusImpl) ChainComputeBaseFee(ctx context.Context, tsk types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().ChainComputeBaseFee(ctx, tsk)
}
func (c *LotusImpl) SyncProgress(ctx context.Context) (api.SyncProgress, error) {
	return bestNodeApi().SyncProgress(ctx)
}
func (c *LotusImpl) InputWalletStatus(ctx context.Context) (string, error) {
	return bestNodeApi().InputWalletStatus(ctx)
}
func (c *LotusImpl) InputWalletPasswd(ctx context.Context, passwd string) error {
	return bestNodeApi().InputWalletPasswd(ctx, passwd)
}
func (c *LotusImpl) WalletEncode(ctx context.Context, addr address.Address, passwd string) error {
	return bestNodeApi().WalletEncode(ctx, addr, passwd)
}
func (s *LotusImpl) StateGetBeaconEntry(p0 context.Context, p1 abi.ChainEpoch) (*types.BeaconEntry, error) {
	return bestNodeApi().StateGetBeaconEntry(p0, p1)
}

func (s *LotusImpl) ChainDeleteObj(p0 context.Context, p1 cid.Cid) error {
	return bestNodeApi().ChainDeleteObj(p0, p1)
}

func (s *LotusImpl) ChainExport(p0 context.Context, p1 abi.ChainEpoch, p2 bool, p3 types.TipSetKey) (<-chan []byte, error) {
	return bestNodeApi().ChainExport(p0, p1, p2, p3)
}

func (s *LotusImpl) ChainGetBlock(p0 context.Context, p1 cid.Cid) (*types.BlockHeader, error) {
	return bestNodeApi().ChainGetBlock(p0, p1)
}

func (s *LotusImpl) ChainGetBlockMessages(p0 context.Context, p1 cid.Cid) (*api.BlockMessages, error) {
	return bestNodeApi().ChainGetBlockMessages(p0, p1)
}

func (s *LotusImpl) ChainGetGenesis(p0 context.Context) (*types.TipSet, error) {
	return bestNodeApi().ChainGetGenesis(p0)
}

func (s *LotusImpl) ChainGetMessage(p0 context.Context, p1 cid.Cid) (*types.Message, error) {
	return bestNodeApi().ChainGetMessage(p0, p1)
}

func (s *LotusImpl) ChainGetNode(p0 context.Context, p1 string) (*api.IpldObject, error) {
	return bestNodeApi().ChainGetNode(p0, p1)
}

func (s *LotusImpl) ChainGetParentMessages(p0 context.Context, p1 cid.Cid) ([]api.Message, error) {
	return bestNodeApi().ChainGetParentMessages(p0, p1)
}

func (s *LotusImpl) ChainGetParentReceipts(p0 context.Context, p1 cid.Cid) ([]*types.MessageReceipt, error) {
	return bestNodeApi().ChainGetParentReceipts(p0, p1)
}

func (s *LotusImpl) ChainGetPath(p0 context.Context, p1 types.TipSetKey, p2 types.TipSetKey) ([]*api.HeadChange, error) {
	return bestNodeApi().ChainGetPath(p0, p1, p2)
}
func (s *LotusImpl) StateGetRandomnessFromTickets(p0 context.Context, p1 crypto.DomainSeparationTag, p2 abi.ChainEpoch, p3 []byte, p4 types.TipSetKey) (abi.Randomness, error) {
	return bestNodeApi().StateGetRandomnessFromTickets(p0, p1, p2, p3, p4)
}

// StateGetRandomnessFromBeacon is used to sample the beacon for randomness.
func (s *LotusImpl) StateGetRandomnessFromBeacon(p0 context.Context, p1 crypto.DomainSeparationTag, p2 abi.ChainEpoch, p3 []byte, p4 types.TipSetKey) (abi.Randomness, error) {
	return bestNodeApi().StateGetRandomnessFromBeacon(p0, p1, p2, p3, p4)
}
func (s *LotusImpl) ChainGetTipSet(p0 context.Context, p1 types.TipSetKey) (*types.TipSet, error) {
	return bestNodeApi().ChainGetTipSet(p0, p1)
}

func (s *LotusImpl) ChainGetTipSetByHeight(p0 context.Context, p1 abi.ChainEpoch, p2 types.TipSetKey) (*types.TipSet, error) {
	return bestNodeApi().ChainGetTipSetByHeight(p0, p1, p2)
}
func (s *LotusImpl) ChainGetTipSetAfterHeight(p0 context.Context, p1 abi.ChainEpoch, p2 types.TipSetKey) (*types.TipSet, error) {
	return bestNodeApi().ChainGetTipSetAfterHeight(p0, p1, p2)
}
func (s *LotusImpl) ChainHasObj(p0 context.Context, p1 cid.Cid) (bool, error) {
	return bestNodeApi().ChainHasObj(p0, p1)
}

func (s *LotusImpl) ChainHead(p0 context.Context) (*types.TipSet, error) {
	return bestNodeApi().ChainHead(p0)
}

func (s *LotusImpl)ChainPutObj(p0 context.Context, p1 blocks.Block) error {
	return bestNodeApi().ChainPutObj(p0,p1)
}
func (s *LotusImpl) ChainReadObj(p0 context.Context, p1 cid.Cid) ([]byte, error) {
	return bestNodeApi().ChainReadObj(p0, p1)
}

func (s *LotusImpl) ChainSetHead(p0 context.Context, p1 types.TipSetKey) error {
	return bestNodeApi().ChainSetHead(p0, p1)
}

func (s *LotusImpl) ChainStatObj(p0 context.Context, p1 cid.Cid, p2 cid.Cid) (api.ObjStat, error) {
	return bestNodeApi().ChainStatObj(p0, p1, p2)
}

func (s *LotusImpl) ChainTipSetWeight(p0 context.Context, p1 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().ChainTipSetWeight(p0, p1)
}

func (s *LotusImpl) ClientCalcCommP(p0 context.Context, p1 string) (*api.CommPRet, error) {
	return bestNodeApi().ClientCalcCommP(p0, p1)
}

func (s *LotusImpl) ClientCancelDataTransfer(p0 context.Context, p1 datatransfer.TransferID, p2 peer.ID, p3 bool) error {
	return bestNodeApi().ClientCancelDataTransfer(p0, p1, p2, p3)
}

func (s *LotusImpl) ClientCancelRetrievalDeal(p0 context.Context, p1 retrievalmarket.DealID) error {
	return bestNodeApi().ClientCancelRetrievalDeal(p0, p1)
}

func (s *LotusImpl) ClientDataTransferUpdates(p0 context.Context) (<-chan api.DataTransferChannel, error) {
	return bestNodeApi().ClientDataTransferUpdates(p0)
}

func (s *LotusImpl) ClientDealPieceCID(p0 context.Context, p1 cid.Cid) (api.DataCIDSize, error) {
	return bestNodeApi().ClientDealPieceCID(p0, p1)
}

func (s *LotusImpl) ClientDealSize(p0 context.Context, p1 cid.Cid) (api.DataSize, error) {
	return bestNodeApi().ClientDealSize(p0, p1)
}

func (s *LotusImpl) ClientFindData(p0 context.Context, p1 cid.Cid, p2 *cid.Cid) ([]api.QueryOffer, error) {
	return bestNodeApi().ClientFindData(p0, p1, p2)
}

func (s *LotusImpl) ClientGenCar(p0 context.Context, p1 api.FileRef, p2 string) error {
	return bestNodeApi().ClientGenCar(p0, p1, p2)
}

func (s *LotusImpl) ClientGetDealInfo(p0 context.Context, p1 cid.Cid) (*api.DealInfo, error) {
	return bestNodeApi().ClientGetDealInfo(p0, p1)
}

func (s *LotusImpl) ClientGetDealStatus(p0 context.Context, p1 uint64) (string, error) {
	return bestNodeApi().ClientGetDealStatus(p0, p1)
}

func (s *LotusImpl) ClientGetDealUpdates(p0 context.Context) (<-chan api.DealInfo, error) {
	return bestNodeApi().ClientGetDealUpdates(p0)
}

func (s *LotusImpl) ClientHasLocal(p0 context.Context, p1 cid.Cid) (bool, error) {
	return bestNodeApi().ClientHasLocal(p0, p1)
}

func (s *LotusImpl) ClientImport(p0 context.Context, p1 api.FileRef) (*api.ImportRes, error) {
	return bestNodeApi().ClientImport(p0, p1)
}

func (s *LotusImpl) ClientListDataTransfers(p0 context.Context) ([]api.DataTransferChannel, error) {
	return bestNodeApi().ClientListDataTransfers(p0)
}

func (s *LotusImpl) ClientListDeals(p0 context.Context) ([]api.DealInfo, error) {
	return bestNodeApi().ClientListDeals(p0)
}

func (s *LotusImpl) ClientListImports(p0 context.Context) ([]api.Import, error) {
	return bestNodeApi().ClientListImports(p0)
}

func (s *LotusImpl) ClientMinerQueryOffer(p0 context.Context, p1 address.Address, p2 cid.Cid, p3 *cid.Cid) (api.QueryOffer, error) {
	return bestNodeApi().ClientMinerQueryOffer(p0, p1, p2, p3)
}

func (s *LotusImpl) ClientQueryAsk(p0 context.Context, p1 peer.ID, p2 address.Address) (*api.StorageAsk, error) {
	return bestNodeApi().ClientQueryAsk(p0, p1, p2)
}

func (s *LotusImpl) ClientRemoveImport(p0 context.Context, p1 imports.ID) error {
	return bestNodeApi().ClientRemoveImport(p0, p1)
}
func (s *LotusImpl) ClientExport(p0 context.Context, p1 api.ExportRef, p2 api.FileRef) error {
	return bestNodeApi().ClientExport(p0, p1, p2)
}
func (s *LotusImpl) ClientRestartDataTransfer(p0 context.Context, p1 datatransfer.TransferID, p2 peer.ID, p3 bool) error {
	return bestNodeApi().ClientRestartDataTransfer(p0, p1, p2, p3)
}

func (s *LotusImpl) ClientRetrieve(p0 context.Context, p1 api.RetrievalOrder) (*api.RestrievalRes, error) {
	return bestNodeApi().ClientRetrieve(p0, p1)
}

func (s *LotusImpl) ClientRetrieveTryRestartInsufficientFunds(p0 context.Context, p1 address.Address) error {
	return bestNodeApi().ClientRetrieveTryRestartInsufficientFunds(p0, p1)
}

func (s *LotusImpl) ClientRetrieveWait(p0 context.Context, p1 retrievalmarket.DealID) error {
	return bestNodeApi().ClientRetrieveWait(p0, p1)
}
func (s *LotusImpl) MsigCancelTxnHash(p0 context.Context, p1 address.Address, p2 uint64, p3 address.Address, p4 types.BigInt, p5 address.Address, p6 uint64, p7 []byte) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigCancelTxnHash(p0, p1, p2, p3, p4, p5, p6, p7)
}

// ClientListRetrievals returns information about retrievals made by the local client
func (s *LotusImpl) ClientListRetrievals(p0 context.Context) ([]api.RetrievalInfo, error) {
	return bestNodeApi().ClientListRetrievals(p0)
}

// ClientGetRetrievalUpdates returns status of updated retrieval deals
func (s *LotusImpl) ClientGetRetrievalUpdates(p0 context.Context) (<-chan api.RetrievalInfo, error) {
	return bestNodeApi().ClientGetRetrievalUpdates(p0)
}
func (s *LotusImpl) ClientStartDeal(p0 context.Context, p1 *api.StartDealParams) (*cid.Cid, error) {
	return bestNodeApi().ClientStartDeal(p0, p1)
}

func (s *LotusImpl) ClientStatelessDeal(p0 context.Context, p1 *api.StartDealParams) (*cid.Cid, error) {
	return bestNodeApi().ClientStatelessDeal(p0, p1)
}

func (s *LotusImpl) CreateBackup(p0 context.Context, p1 string) error {
	return bestNodeApi().CreateBackup(p0, p1)
}

func (s *LotusImpl) GasEstimateFeeCap(p0 context.Context, p1 *types.Message, p2 int64, p3 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().GasEstimateFeeCap(p0, p1, p2, p3)
}

func (s *LotusImpl) GasEstimateGasLimit(p0 context.Context, p1 *types.Message, p2 types.TipSetKey) (int64, error) {
	return bestNodeApi().GasEstimateGasLimit(p0, p1, p2)
}

func (s *LotusImpl) GasEstimateGasPremium(p0 context.Context, p1 uint64, p2 address.Address, p3 int64, p4 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().GasEstimateGasPremium(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) GasEstimateMessageGas(p0 context.Context, p1 *types.Message, p2 *api.MessageSendSpec, p3 types.TipSetKey) (*types.Message, error) {
	return bestNodeApi().GasEstimateMessageGas(p0, p1, p2, p3)
}

func (s *LotusImpl) MarketAddBalance(p0 context.Context, p1 address.Address, p2 address.Address, p3 types.BigInt) (cid.Cid, error) {
	return bestNodeApi().MarketAddBalance(p0, p1, p2, p3)
}

func (s *LotusImpl) MarketGetReserved(p0 context.Context, p1 address.Address) (types.BigInt, error) {
	return bestNodeApi().MarketGetReserved(p0, p1)
}

func (s *LotusImpl) MarketReleaseFunds(p0 context.Context, p1 address.Address, p2 types.BigInt) error {
	return bestNodeApi().MarketReleaseFunds(p0, p1, p2)
}

func (s *LotusImpl) MarketReserveFunds(p0 context.Context, p1 address.Address, p2 address.Address, p3 types.BigInt) (cid.Cid, error) {
	return bestNodeApi().MarketReserveFunds(p0, p1, p2, p3)
}

func (s *LotusImpl) MarketWithdraw(p0 context.Context, p1 address.Address, p2 address.Address, p3 types.BigInt) (cid.Cid, error) {
	return bestNodeApi().MarketWithdraw(p0, p1, p2, p3)
}

func (s *LotusImpl) MinerCreateBlock(p0 context.Context, p1 *api.BlockTemplate) (*types.BlockMsg, error) {
	return bestNodeApi().MinerCreateBlock(p0, p1)
}

func (s *LotusImpl) MinerGetBaseInfo(p0 context.Context, p1 address.Address, p2 abi.ChainEpoch, p3 types.TipSetKey) (*api.MiningBaseInfo, error) {
	return bestNodeApi().MinerGetBaseInfo(p0, p1, p2, p3)
}

func (s *LotusImpl) MpoolBatchPush(p0 context.Context, p1 []*types.SignedMessage) ([]cid.Cid, error) {
	return bestNodeApi().MpoolBatchPush(p0, p1)
}

func (s *LotusImpl) MpoolBatchPushMessage(p0 context.Context, p1 []*types.Message, p2 *api.MessageSendSpec) ([]*types.SignedMessage, error) {
	return bestNodeApi().MpoolBatchPushMessage(p0, p1, p2)
}

func (s *LotusImpl) MpoolBatchPushUntrusted(p0 context.Context, p1 []*types.SignedMessage) ([]cid.Cid, error) {
	return bestNodeApi().MpoolBatchPushUntrusted(p0, p1)
}

func (s *LotusImpl) MpoolCheckMessages(p0 context.Context, p1 []*api.MessagePrototype) ([][]api.MessageCheckStatus, error) {
	return bestNodeApi().MpoolCheckMessages(p0, p1)
}

func (s *LotusImpl) MpoolCheckPendingMessages(p0 context.Context, p1 address.Address) ([][]api.MessageCheckStatus, error) {
	return bestNodeApi().MpoolCheckPendingMessages(p0, p1)
}

func (s *LotusImpl) MpoolCheckReplaceMessages(p0 context.Context, p1 []*types.Message) ([][]api.MessageCheckStatus, error) {
	return bestNodeApi().MpoolCheckReplaceMessages(p0, p1)
}

func (s *LotusImpl) MpoolClear(p0 context.Context, p1 bool) error {
	return bestNodeApi().MpoolClear(p0, p1)
}

func (s *LotusImpl) MpoolGetConfig(p0 context.Context) (*types.MpoolConfig, error) {
	return bestNodeApi().MpoolGetConfig(p0)
}

func (s *LotusImpl) MpoolGetNonce(p0 context.Context, p1 address.Address) (uint64, error) {
	return bestNodeApi().MpoolGetNonce(p0, p1)
}

func (s *LotusImpl) MpoolPending(p0 context.Context, p1 types.TipSetKey) ([]*types.SignedMessage, error) {
	return bestNodeApi().MpoolPending(p0, p1)
}

func (s *LotusImpl) MpoolPushUntrusted(p0 context.Context, p1 *types.SignedMessage) (cid.Cid, error) {
	return bestNodeApi().MpoolPushUntrusted(p0, p1)
}

func (s *LotusImpl) MpoolSelect(p0 context.Context, p1 types.TipSetKey, p2 float64) ([]*types.SignedMessage, error) {
	return bestNodeApi().MpoolSelect(p0, p1, p2)
}

func (s *LotusImpl) MpoolSetConfig(p0 context.Context, p1 *types.MpoolConfig) error {
	return bestNodeApi().MpoolSetConfig(p0, p1)
}

func (s *LotusImpl) MpoolSub(p0 context.Context) (<-chan api.MpoolUpdate, error) {
	return bestNodeApi().MpoolSub(p0)
}

func (s *LotusImpl) MsigAddApprove(p0 context.Context, p1 address.Address, p2 address.Address, p3 uint64, p4 address.Address, p5 address.Address, p6 bool) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigAddApprove(p0, p1, p2, p3, p4, p5, p6)
}

func (s *LotusImpl) MsigAddCancel(p0 context.Context, p1 address.Address, p2 address.Address, p3 uint64, p4 address.Address, p5 bool) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigAddCancel(p0, p1, p2, p3, p4, p5)
}

func (s *LotusImpl) MsigAddPropose(p0 context.Context, p1 address.Address, p2 address.Address, p3 address.Address, p4 bool) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigAddPropose(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) MsigApprove(p0 context.Context, p1 address.Address, p2 uint64, p3 address.Address) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigApprove(p0, p1, p2, p3)
}

func (s *LotusImpl) MsigApproveTxnHash(p0 context.Context, p1 address.Address, p2 uint64, p3 address.Address, p4 address.Address, p5 types.BigInt, p6 address.Address, p7 uint64, p8 []byte) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigApproveTxnHash(p0, p1, p2, p3, p4, p5, p6, p7, p8)
}
func (s *LotusImpl) MsigCancel(p0 context.Context, p1 address.Address, p2 uint64, p3 address.Address) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigCancel(p0, p1, p2, p3)
}

func (s *LotusImpl) MsigCreate(p0 context.Context, p1 uint64, p2 []address.Address, p3 abi.ChainEpoch, p4 types.BigInt, p5 address.Address, p6 types.BigInt) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigCreate(p0, p1, p2, p3, p4, p5, p6)
}

func (s *LotusImpl) MsigGetAvailableBalance(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().MsigGetAvailableBalance(p0, p1, p2)
}

func (s *LotusImpl) MsigGetPending(p0 context.Context, p1 address.Address, p2 types.TipSetKey) ([]*api.MsigTransaction, error) {
	return bestNodeApi().MsigGetPending(p0, p1, p2)
}

func (s *LotusImpl) MsigGetVested(p0 context.Context, p1 address.Address, p2 types.TipSetKey, p3 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().MsigGetVested(p0, p1, p2, p3)
}

func (s *LotusImpl) MsigGetVestingSchedule(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (api.MsigVesting, error) {
	return bestNodeApi().MsigGetVestingSchedule(p0, p1, p2)
}

func (s *LotusImpl) MsigPropose(p0 context.Context, p1 address.Address, p2 address.Address, p3 types.BigInt, p4 address.Address, p5 uint64, p6 []byte) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigPropose(p0, p1, p2, p3, p4, p5, p6)
}

func (s *LotusImpl) MsigRemoveSigner(p0 context.Context, p1 address.Address, p2 address.Address, p3 address.Address, p4 bool) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigRemoveSigner(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) MsigSwapApprove(p0 context.Context, p1 address.Address, p2 address.Address, p3 uint64, p4 address.Address, p5 address.Address, p6 address.Address) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigSwapApprove(p0, p1, p2, p3, p4, p5, p6)
}

func (s *LotusImpl) MsigSwapCancel(p0 context.Context, p1 address.Address, p2 address.Address, p3 uint64, p4 address.Address, p5 address.Address) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigSwapCancel(p0, p1, p2, p3, p4, p5)
}

func (s *LotusImpl) MsigSwapPropose(p0 context.Context, p1 address.Address, p2 address.Address, p3 address.Address, p4 address.Address) (*api.MessagePrototype, error) {
	return bestNodeApi().MsigSwapPropose(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) NodeStatus(p0 context.Context, p1 bool) (api.NodeStatus, error) {
	return bestNodeApi().NodeStatus(p0, p1)
}

func (s *LotusImpl) PaychAllocateLane(p0 context.Context, p1 address.Address) (uint64, error) {
	return bestNodeApi().PaychAllocateLane(p0, p1)
}

func (s *LotusImpl) PaychAvailableFunds(p0 context.Context, p1 address.Address) (*api.ChannelAvailableFunds, error) {
	return bestNodeApi().PaychAvailableFunds(p0, p1)
}

func (s *LotusImpl) PaychAvailableFundsByFromTo(p0 context.Context, p1 address.Address, p2 address.Address) (*api.ChannelAvailableFunds, error) {
	return bestNodeApi().PaychAvailableFundsByFromTo(p0, p1, p2)
}

func (s *LotusImpl) PaychCollect(p0 context.Context, p1 address.Address) (cid.Cid, error) {
	return bestNodeApi().PaychCollect(p0, p1)
}

func (s *LotusImpl) PaychGetWaitReady(p0 context.Context, p1 cid.Cid) (address.Address, error) {
	return bestNodeApi().PaychGetWaitReady(p0, p1)
}

func (s *LotusImpl) PaychList(p0 context.Context) ([]address.Address, error) {
	return bestNodeApi().PaychList(p0)
}

func (s *LotusImpl) PaychNewPayment(p0 context.Context, p1 address.Address, p2 address.Address, p3 []api.VoucherSpec) (*api.PaymentInfo, error) {
	return bestNodeApi().PaychNewPayment(p0, p1, p2, p3)
}

func (s *LotusImpl) PaychSettle(p0 context.Context, p1 address.Address) (cid.Cid, error) {
	return bestNodeApi().PaychSettle(p0, p1)
}

func (s *LotusImpl) PaychStatus(p0 context.Context, p1 address.Address) (*api.PaychStatus, error) {
	return bestNodeApi().PaychStatus(p0, p1)
}

func (s *LotusImpl) PaychVoucherAdd(p0 context.Context, p1 address.Address, p2 *paych.SignedVoucher, p3 []byte, p4 types.BigInt) (types.BigInt, error) {
	return bestNodeApi().PaychVoucherAdd(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) PaychVoucherCheckSpendable(p0 context.Context, p1 address.Address, p2 *paych.SignedVoucher, p3 []byte, p4 []byte) (bool, error) {
	return bestNodeApi().PaychVoucherCheckSpendable(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) PaychVoucherCheckValid(p0 context.Context, p1 address.Address, p2 *paych.SignedVoucher) error {
	return bestNodeApi().PaychVoucherCheckValid(p0, p1, p2)
}

func (s *LotusImpl) PaychVoucherCreate(p0 context.Context, p1 address.Address, p2 types.BigInt, p3 uint64) (*api.VoucherCreateResult, error) {
	return bestNodeApi().PaychVoucherCreate(p0, p1, p2, p3)
}

func (s *LotusImpl) PaychVoucherList(p0 context.Context, p1 address.Address) ([]*paych.SignedVoucher, error) {
	return bestNodeApi().PaychVoucherList(p0, p1)
}

func (s *LotusImpl) PaychVoucherSubmit(p0 context.Context, p1 address.Address, p2 *paych.SignedVoucher, p3 []byte, p4 []byte) (cid.Cid, error) {
	return bestNodeApi().PaychVoucherSubmit(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) StateAccountKey(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (address.Address, error) {
	return bestNodeApi().StateAccountKey(p0, p1, p2)
}

func (s *LotusImpl) StateAllMinerFaults(p0 context.Context, p1 abi.ChainEpoch, p2 types.TipSetKey) ([]*api.Fault, error) {
	return bestNodeApi().StateAllMinerFaults(p0, p1, p2)
}

func (s *LotusImpl) StateCall(p0 context.Context, p1 *types.Message, p2 types.TipSetKey) (*api.InvocResult, error) {
	return bestNodeApi().StateCall(p0, p1, p2)
}

func (s *LotusImpl) StateChangedActors(p0 context.Context, p1 cid.Cid, p2 cid.Cid) (map[string]types.Actor, error) {
	return bestNodeApi().StateChangedActors(p0, p1, p2)
}

func (s *LotusImpl) StateCirculatingSupply(p0 context.Context, p1 types.TipSetKey) (abi.TokenAmount, error) {
	return bestNodeApi().StateCirculatingSupply(p0, p1)
}

func (s *LotusImpl) StateCompute(p0 context.Context, p1 abi.ChainEpoch, p2 []*types.Message, p3 types.TipSetKey) (*api.ComputeStateOutput, error) {
	return bestNodeApi().StateCompute(p0, p1, p2, p3)
}

func (s *LotusImpl) StateDealProviderCollateralBounds(p0 context.Context, p1 abi.PaddedPieceSize, p2 bool, p3 types.TipSetKey) (api.DealCollateralBounds, error) {
	return bestNodeApi().StateDealProviderCollateralBounds(p0, p1, p2, p3)
}

func (s *LotusImpl) StateDecodeParams(p0 context.Context, p1 address.Address, p2 abi.MethodNum, p3 []byte, p4 types.TipSetKey) (interface{}, error) {
	return bestNodeApi().StateDecodeParams(p0, p1, p2, p3, p4)
}
func (s *LotusImpl) StateEncodeParams(p0 context.Context, p1 cid.Cid, p2 abi.MethodNum, p3 json.RawMessage) ([]byte, error) {
	return bestNodeApi().StateEncodeParams(p0, p1, p2, p3)
}
func (s *LotusImpl) StateGetActor(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*types.Actor, error) {
	return bestNodeApi().StateGetActor(p0, p1, p2)
}

func (s *LotusImpl) StateListActors(p0 context.Context, p1 types.TipSetKey) ([]address.Address, error) {
	return bestNodeApi().StateListActors(p0, p1)
}

func (s *LotusImpl) StateListMessages(p0 context.Context, p1 *api.MessageMatch, p2 types.TipSetKey, p3 abi.ChainEpoch) ([]cid.Cid, error) {
	return bestNodeApi().StateListMessages(p0, p1, p2, p3)
}

func (s *LotusImpl) StateListMiners(p0 context.Context, p1 types.TipSetKey) ([]address.Address, error) {
	return bestNodeApi().StateListMiners(p0, p1)
}

func (s *LotusImpl) StateLookupID(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (address.Address, error) {
	return bestNodeApi().StateLookupID(p0, p1, p2)
}
func (s *LotusImpl) StateActorCodeCIDs(p0 context.Context, p1 abinetwork.Version) (map[string]cid.Cid, error) {
	return bestNodeApi().StateActorCodeCIDs(p0, p1)
}
func (s *LotusImpl) StateMarketBalance(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (api.MarketBalance, error) {
	return bestNodeApi().StateMarketBalance(p0, p1, p2)
}

func (s *LotusImpl) StateMarketDeals(p0 context.Context, p1 types.TipSetKey) (map[string]*api.MarketDeal, error) {
	return bestNodeApi().StateMarketDeals(p0, p1)
}

func (s *LotusImpl) StateMarketParticipants(p0 context.Context, p1 types.TipSetKey) (map[string]api.MarketBalance, error) {
	return bestNodeApi().StateMarketParticipants(p0, p1)
}

func (s *LotusImpl) StateMarketStorageDeal(p0 context.Context, p1 abi.DealID, p2 types.TipSetKey) (*api.MarketDeal, error) {
	return bestNodeApi().StateMarketStorageDeal(p0, p1, p2)
}

func (s *LotusImpl) StateMinerActiveSectors(p0 context.Context, p1 address.Address, p2 types.TipSetKey) ([]*miner.SectorOnChainInfo, error) {
	return bestNodeApi().StateMinerActiveSectors(p0, p1, p2)
}

func (s *LotusImpl) StateMinerAvailableBalance(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().StateMinerAvailableBalance(p0, p1, p2)
}

func (s *LotusImpl) StateMinerDeadlines(p0 context.Context, p1 address.Address, p2 types.TipSetKey) ([]api.Deadline, error) {
	return bestNodeApi().StateMinerDeadlines(p0, p1, p2)
}

func (s *LotusImpl) StateMinerFaults(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (bitfield.BitField, error) {
	return bestNodeApi().StateMinerFaults(p0, p1, p2)
}

func (s *LotusImpl) StateMinerInfo(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (api.MinerInfo, error) {
	return bestNodeApi().StateMinerInfo(p0, p1, p2)
}

func (s *LotusImpl) StateMinerInitialPledgeCollateral(p0 context.Context, p1 address.Address, p2 miner.SectorPreCommitInfo, p3 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().StateMinerInitialPledgeCollateral(p0, p1, p2, p3)
}

func (s *LotusImpl) StateMinerPartitions(p0 context.Context, p1 address.Address, p2 uint64, p3 types.TipSetKey) ([]api.Partition, error) {
	return bestNodeApi().StateMinerPartitions(p0, p1, p2, p3)
}

func (s *LotusImpl) StateMinerPower(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*api.MinerPower, error) {
	return bestNodeApi().StateMinerPower(p0, p1, p2)
}

func (s *LotusImpl) StateMinerPreCommitDepositForPower(p0 context.Context, p1 address.Address, p2 miner.SectorPreCommitInfo, p3 types.TipSetKey) (types.BigInt, error) {
	return bestNodeApi().StateMinerPreCommitDepositForPower(p0, p1, p2, p3)
}

func (s *LotusImpl) StateMinerProvingDeadline(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*dline.Info, error) {
	return bestNodeApi().StateMinerProvingDeadline(p0, p1, p2)
}

func (s *LotusImpl) StateMinerRecoveries(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (bitfield.BitField, error) {
	return bestNodeApi().StateMinerRecoveries(p0, p1, p2)
}

func (s *LotusImpl) StateMinerSectorAllocated(p0 context.Context, p1 address.Address, p2 abi.SectorNumber, p3 types.TipSetKey) (bool, error) {
	return bestNodeApi().StateMinerSectorAllocated(p0, p1, p2, p3)
}

func (s *LotusImpl) StateMinerSectorCount(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (api.MinerSectors, error) {
	return bestNodeApi().StateMinerSectorCount(p0, p1, p2)
}

func (s *LotusImpl) StateMinerSectors(p0 context.Context, p1 address.Address, p2 *bitfield.BitField, p3 types.TipSetKey) ([]*miner.SectorOnChainInfo, error) {
	return bestNodeApi().StateMinerSectors(p0, p1, p2, p3)
}

func (s *LotusImpl) StateNetworkName(p0 context.Context) (dtypes.NetworkName, error) {
	return bestNodeApi().StateNetworkName(p0)
}

func (s *LotusImpl) StateNetworkVersion(p0 context.Context, p1 types.TipSetKey) (apitypes.NetworkVersion, error) {
	return bestNodeApi().StateNetworkVersion(p0, p1)
}

func (s *LotusImpl) StateReadState(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*api.ActorState, error) {
	return bestNodeApi().StateReadState(p0, p1, p2)
}

func (s *LotusImpl) StateReplay(p0 context.Context, p1 types.TipSetKey, p2 cid.Cid) (*api.InvocResult, error) {
	return bestNodeApi().StateReplay(p0, p1, p2)
}

func (s *LotusImpl) StateSearchMsg(p0 context.Context, p1 types.TipSetKey, p2 cid.Cid, p3 abi.ChainEpoch, p4 bool) (*api.MsgLookup, error) {
	return bestNodeApi().StateSearchMsg(p0, p1, p2, p3, p4)
}

func (s *LotusImpl) StateSectorExpiration(p0 context.Context, p1 address.Address, p2 abi.SectorNumber, p3 types.TipSetKey) (*lminer.SectorExpiration, error) {
	return bestNodeApi().StateSectorExpiration(p0, p1, p2, p3)
}

func (s *LotusImpl) StateSectorGetInfo(p0 context.Context, p1 address.Address, p2 abi.SectorNumber, p3 types.TipSetKey) (*miner.SectorOnChainInfo, error) {
	return bestNodeApi().StateSectorGetInfo(p0, p1, p2, p3)
}

func (s *LotusImpl) StateSectorPartition(p0 context.Context, p1 address.Address, p2 abi.SectorNumber, p3 types.TipSetKey) (*lminer.SectorLocation, error) {
	return bestNodeApi().StateSectorPartition(p0, p1, p2, p3)
}

func (s *LotusImpl)StateSectorPreCommitInfo(p0 context.Context, p1 address.Address,p2 abi.SectorNumber,p3 types.TipSetKey) (*miner.SectorPreCommitOnChainInfo, error) {
	return bestNodeApi().StateSectorPreCommitInfo(p0, p1, p2, p3)
}
func (s *LotusImpl) StateMinerAllocated(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*bitfield.BitField, error) {
	return bestNodeApi().StateMinerAllocated(p0, p1, p2)
}

func (s *LotusImpl) StateActorManifestCID(p0 context.Context, p1 abinetwork.Version) (cid.Cid, error) {
	return bestNodeApi().StateActorManifestCID(p0, p1)
}

func (s *LotusImpl) StateVMCirculatingSupplyInternal(p0 context.Context, p1 types.TipSetKey) (api.CirculatingSupply, error) {
	return bestNodeApi().StateVMCirculatingSupplyInternal(p0, p1)
}

func (s *LotusImpl) StateVerifiedClientStatus(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*abi.StoragePower, error) {
	return bestNodeApi().StateVerifiedClientStatus(p0, p1, p2)
}

func (s *LotusImpl) StateVerifiedRegistryRootKey(p0 context.Context, p1 types.TipSetKey) (address.Address, error) {
	return bestNodeApi().StateVerifiedRegistryRootKey(p0, p1)
}

func (s *LotusImpl) StateVerifierStatus(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (*abi.StoragePower, error) {
	return bestNodeApi().StateVerifierStatus(p0, p1, p2)
}

func (s *LotusImpl) SyncCheckBad(p0 context.Context, p1 cid.Cid) (string, error) {
	return bestNodeApi().SyncCheckBad(p0, p1)
}

func (s *LotusImpl) SyncCheckpoint(p0 context.Context, p1 types.TipSetKey) error {
	return bestNodeApi().SyncCheckpoint(p0, p1)
}

func (s *LotusImpl) SyncIncomingBlocks(p0 context.Context) (<-chan *types.BlockHeader, error) {
	return bestNodeApi().SyncIncomingBlocks(p0)
}

func (s *LotusImpl) SyncMarkBad(p0 context.Context, p1 cid.Cid) error {
	return bestNodeApi().SyncMarkBad(p0, p1)
}

func (s *LotusImpl) SyncState(p0 context.Context) (*api.SyncState, error) {
	return bestNodeApi().SyncState(p0)
}

func (s *LotusImpl) SyncSubmitBlock(p0 context.Context, p1 *types.BlockMsg) error {
	return bestNodeApi().SyncSubmitBlock(p0, p1)
}

func (s *LotusImpl) SyncUnmarkAllBad(p0 context.Context) error {
	return bestNodeApi().SyncUnmarkAllBad(p0)
}

func (s *LotusImpl) SyncUnmarkBad(p0 context.Context, p1 cid.Cid) error {
	return bestNodeApi().SyncUnmarkBad(p0, p1)
}

func (s *LotusImpl) SyncValidateTipset(p0 context.Context, p1 types.TipSetKey) (bool, error) {
	return bestNodeApi().SyncValidateTipset(p0, p1)
}

func (s *LotusImpl) WalletBalance(p0 context.Context, p1 address.Address) (types.BigInt, error) {
	return bestNodeApi().WalletBalance(p0, p1)
}

func (s *LotusImpl) WalletDefaultAddress(p0 context.Context) (address.Address, error) {
	return bestNodeApi().WalletDefaultAddress(p0)
}

func (s *LotusImpl) WalletDelete(p0 context.Context, p1 address.Address) error {
	return bestNodeApi().WalletDelete(p0, p1)
}

func (s *LotusImpl) WalletExport(p0 context.Context, p1 address.Address) (*types.KeyInfo, error) {
	return bestNodeApi().WalletExport(p0, p1)
}

func (s *LotusImpl) WalletHas(p0 context.Context, p1 address.Address) (bool, error) {
	return bestNodeApi().WalletHas(p0, p1)
}

func (s *LotusImpl) WalletImport(p0 context.Context, p1 *types.KeyInfo) (address.Address, error) {
	return bestNodeApi().WalletImport(p0, p1)
}

func (s *LotusImpl) WalletList(p0 context.Context) ([]address.Address, error) {
	return bestNodeApi().WalletList(p0)
}

func (s *LotusImpl) WalletNew(p0 context.Context, p1 types.KeyType, p2 string) (address.Address, error) {
	return bestNodeApi().WalletNew(p0, p1, p2)
}

func (s *LotusImpl) WalletSetDefault(p0 context.Context, p1 address.Address) error {
	return bestNodeApi().WalletSetDefault(p0, p1)
}

func (s *LotusImpl) WalletSign(p0 context.Context, p1 address.Address, p2 []byte) (*crypto.Signature, error) {
	return bestNodeApi().WalletSign(p0, p1, p2)
}

func (s *LotusImpl) WalletSignMessage(p0 context.Context, p1 address.Address, p2 *types.Message) (*types.SignedMessage, error) {
	return bestNodeApi().WalletSignMessage(p0, p1, p2)
}

func (s *LotusImpl) WalletValidateAddress(p0 context.Context, p1 string) (address.Address, error) {
	return bestNodeApi().WalletValidateAddress(p0, p1)
}

func (s *LotusImpl) WalletVerify(p0 context.Context, p1 address.Address, p2 []byte, p3 *crypto.Signature) (bool, error) {
	return bestNodeApi().WalletVerify(p0, p1, p2, p3)
}
func (s *LotusImpl) ChainCheckBlockstore(p0 context.Context) error {
	return bestNodeApi().ChainCheckBlockstore(p0)
}
func (s *LotusImpl) ChainBlockstoreInfo(p0 context.Context) (map[string]interface{}, error) {
	return bestNodeApi().ChainBlockstoreInfo(p0)
}

// ChainGetMessagesInTipset returns message stores in current tipset
func (s *LotusImpl) ChainGetMessagesInTipset(p0 context.Context, p1 types.TipSetKey) ([]api.Message, error) {
	return bestNodeApi().ChainGetMessagesInTipset(p0, p1)
}

// StateGetNetworkParams return current network params
func (s *LotusImpl)StateGetNetworkParams(p0 context.Context) (*api.NetworkParams, error){
	return bestNodeApi().StateGetNetworkParams(p0)
}

// StateLookupRobustAddress returns the public key address of the given ID address for non-account addresses (multisig, miners etc)
func (s *LotusImpl)StateLookupRobustAddress(p0 context.Context, p1 address.Address, p2 types.TipSetKey) (address.Address, error) {
	return bestNodeApi().StateLookupRobustAddress(p0,p1,p2)
}
func (s *LotusImpl) StateComputeDataCID(p0 context.Context, p1 address.Address, p2 abi.RegisteredSealProof, p3 []abi.DealID, p4 types.TipSetKey) (cid.Cid, error)  {
	return bestNodeApi().StateComputeDataCID(p0,p1,p2,p3,p4)
}
func (s *LotusImpl) ChainPrune(ctx context.Context, p1 api.PruneOpts) error {
	return bestNodeApi().ChainPrune(ctx, p1)
}