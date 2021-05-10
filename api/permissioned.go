package api

import (
	"github.com/filecoin-project/go-jsonrpc/auth"
)

const (
	// When changing these, update docs/API.md too

	PermRead  auth.Permission = "read" // default
	PermWrite auth.Permission = "write"
	PermSign  auth.Permission = "sign"  // Use wallet keys for signing
	PermAdmin auth.Permission = "admin" // Manage permissions
)

var AllPermissions = []auth.Permission{PermRead, PermWrite, PermSign, PermAdmin}
var DefaultPerms = []auth.Permission{PermRead}

func PermissionedStorMinerAPI(a StorageMiner) StorageMiner {
	var out StorageMinerStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.CommonStruct.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.HlmMinerProxyStruct.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.HlmMinerProvingStruct.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.HlmMinerSectorStruct.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.HlmMinerStorageStruct.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.HlmMinerWorkerStruct.Internal)
	return &out
}

func PermissionedFullAPI(a FullNode) FullNode {
	var out FullNodeStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.CommonStruct.Internal)
	return &out
}

func PermissionedWorkerAPI(a Worker) Worker {
	var out WorkerStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	return &out
}
func PermissionedHlmMinerSchedulerAPI(a HlmMinerSchedulerAPI) HlmMinerSchedulerAPI {
	var out HlmMinerSchedulerStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	return &out
}
func PermissionedWorkerHlmAPI(a WorkerHlmAPI) WorkerHlmAPI {
	var out WorkerHlmStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	return &out
}

func PermissionedWalletAPI(a Wallet) Wallet {
	var out WalletStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	return &out
}
