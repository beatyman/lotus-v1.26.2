import code from "./code";

export default {
  [code.account]: [
    "Send",
    "Constructor",
    "GetAddress",
  ],
  [code.power]: [
    "Send",
    "Constructor",
    "CreateStorageMiner",
    "SlashConsensusFault",
    "UpdateStorage",
    "GetTotalStorage",
    "PowerLookup",
    "IsMiner",
    "StorageCollateralForSize"
  ],
  [code.market]: [
    "Send",
    "Constructor",
    "WithdrawBalance",
    "AddBalance",
    "CheckLockedBalance",
    "PublishStorageDeals",
    "HandleCronAction",
    "SettleExpiredDeals",
    "ProcessStorageDealsPayment",
    "SlashStorageDealCollateral",
    "GetLastExpirationFromDealIDs",
    "ActivateStorageDeals",
  ],
  [code.miner]: [
    "Send",
    "Constructor",
    "CommitSector",
    "SubmitPost",
    "SlashStorageFault",
    "GetCurrentProvingSet",
    "ArbitrateDeal",
    "DePledge",
    "GetOwner",
    "GetWorkerAddr",
    "GetPower",
    "GetPeerID",
    "GetSectorSize",
    "UpdatePeerID",
    "ChangeWorker",
    "IsSlashed",
    "IsLate",
    "PaymentVerifyInclusion",
    "PaymentVerifySector",
  ],
  [code.multisig]: [
    "Send",
    "Constructor",
    "Propose",
    "Approve",
    "Cancel",
    "ClearCompleted",
    "AddSigner",
    "RemoveSigner",
    "SwapSigner",
    "ChangeRequirement",
  ],
  [code.init]: [
    "Send",
    "Constructor",
    "Exec",
    "GetIdForAddress"
  ],
  [code.paych]: [
    "Send",
    "Constructor",
    "UpdateChannelState",
    "Close",
    "Collect",
  ],
}