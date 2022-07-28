package ffiwrapper

import (
	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
	"io"

	"github.com/filecoin-project/go-state-types/abi"
)

type NullReader struct {
	*io.LimitedReader
}

func NewNullReader(size abi.UnpaddedPieceSize) io.Reader {
	return &NullReader{(io.LimitReader(&nullreader.Reader{}, int64(size))).(*io.LimitedReader)}
}

func (m NullReader) NullBytes() int64 {
	return m.N
}
