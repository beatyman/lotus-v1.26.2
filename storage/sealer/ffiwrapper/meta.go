package ffiwrapper

import (
	"context"
	"github.com/filecoin-project/go-jsonrpc/meta"
)

const (
	MetaSectorKind   = "META-Sector-Kind"
	MetaSectorAction = "META-Sector-Action"

	MetaSealerDealID         = "META-Sealer-Deal-ID"
	MetaSealerDealPublishCID = "META-Sealer-Deal-PublishCid"

	MetaSealerAddPieceKind = "META-Sealer-AddPiece-Kind"
	MetaSealerMoveStorage  = "META-Sealer-MoveStorage"
)

func GetSectorKind(ctx context.Context) (string, bool) {
	return meta.Get(ctx, MetaSectorKind)
}

func GetSectorAction(ctx context.Context) (string, bool) {
	return meta.Get(ctx, MetaSectorAction)
}

func IsAddPieceDeal(ctx context.Context) bool {
	str, ok := meta.Get(ctx, MetaSealerAddPieceKind)
	return ok && str == "deal"
}
func SetAddPiecePad(ctx context.Context) context.Context {
	return meta.Set(ctx, MetaSealerAddPieceKind, "pad")
}

func SetAddPieceDeal(ctx context.Context) context.Context {
	return meta.Set(ctx, MetaSealerAddPieceKind, "deal")
}

func SetAddPiecePacking(ctx context.Context) context.Context {
	return meta.Set(ctx, MetaSealerAddPieceKind, "packing")
}

func SetFsmMoveStorage(ctx context.Context) context.Context {
	return meta.Set(ctx, MetaSealerMoveStorage, "fsm")
}
func GetAddPieceKind(ctx context.Context) string {
	if ctx==nil{
		ctx=context.TODO()
	}
	str, ok := meta.Get(ctx, MetaSealerAddPieceKind)
	if !ok{
		return "packing"
	}
	return str
}
func SetAddPieceKind(ctx context.Context,kind string) context.Context  {
	return meta.Set(ctx, MetaSealerAddPieceKind, kind)
}