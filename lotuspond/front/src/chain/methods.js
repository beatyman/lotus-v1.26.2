export default {
  "account": [
    "Send",
    "Constructor",
    "GetAddress",
  ],
  "smarket": [
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
  "sminer": [
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
  "multisig": [
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
  "init": [
    "Send",
    "Constructor",
    "Exec",
    "GetIdForAddress"
  ],
  "paych": [
    "Send",
    "Constructor",
    "UpdateChannelState",
    "Close",
    "Collect",
  ],
}