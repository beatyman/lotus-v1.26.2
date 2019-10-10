package types

import (
	"fmt"
	"io"
	"math"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

/* This file was generated by github.com/whyrusleeping/cbor-gen */

var _ = xerrors.Errorf

func (t *BlockHeader) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{140}); err != nil {
		return err
	}

	// t.t.Miner (address.Address)
	if err := t.Miner.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Tickets ([]*types.Ticket)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.Tickets)))); err != nil {
		return err
	}
	for _, v := range t.Tickets {
		if err := v.MarshalCBOR(w); err != nil {
			return err
		}
	}

	// t.t.ElectionProof ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.ElectionProof)))); err != nil {
		return err
	}
	if _, err := w.Write(t.ElectionProof); err != nil {
		return err
	}

	// t.t.Parents ([]cid.Cid)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.Parents)))); err != nil {
		return err
	}
	for _, v := range t.Parents {
		if err := cbg.WriteCid(w, v); err != nil {
			return xerrors.Errorf("failed writing cid field t.Parents: %w", err)
		}
	}

	// t.t.ParentWeight (types.BigInt)
	if err := t.ParentWeight.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Height (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Height)); err != nil {
		return err
	}

	// t.t.ParentStateRoot (cid.Cid)

	if err := cbg.WriteCid(w, t.ParentStateRoot); err != nil {
		return xerrors.Errorf("failed to write cid field t.ParentStateRoot: %w", err)
	}

	// t.t.ParentMessageReceipts (cid.Cid)

	if err := cbg.WriteCid(w, t.ParentMessageReceipts); err != nil {
		return xerrors.Errorf("failed to write cid field t.ParentMessageReceipts: %w", err)
	}

	// t.t.Messages (cid.Cid)

	if err := cbg.WriteCid(w, t.Messages); err != nil {
		return xerrors.Errorf("failed to write cid field t.Messages: %w", err)
	}

	// t.t.BLSAggregate (types.Signature)
	if err := t.BLSAggregate.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Timestamp (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Timestamp)); err != nil {
		return err
	}

	// t.t.BlockSig (types.Signature)
	if err := t.BlockSig.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *BlockHeader) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 12 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Miner (address.Address)

	{

		if err := t.Miner.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Tickets ([]*types.Ticket)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Tickets: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}
	if extra > 0 {
		t.Tickets = make([]*Ticket, extra)
	}
	for i := 0; i < int(extra); i++ {

		var v Ticket
		if err := v.UnmarshalCBOR(br); err != nil {
			return err
		}

		t.Tickets[i] = &v
	}

	// t.t.ElectionProof ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.ElectionProof: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.ElectionProof = make([]byte, extra)
	if _, err := io.ReadFull(br, t.ElectionProof); err != nil {
		return err
	}
	// t.t.Parents ([]cid.Cid)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Parents: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}
	if extra > 0 {
		t.Parents = make([]cid.Cid, extra)
	}
	for i := 0; i < int(extra); i++ {

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("reading cid field t.Parents failed: %w", err)
		}
		t.Parents[i] = c
	}

	// t.t.ParentWeight (types.BigInt)

	{

		if err := t.ParentWeight.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Height (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Height = extra
	// t.t.ParentStateRoot (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.ParentStateRoot: %w", err)
		}

		t.ParentStateRoot = c

	}
	// t.t.ParentMessageReceipts (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.ParentMessageReceipts: %w", err)
		}

		t.ParentMessageReceipts = c

	}
	// t.t.Messages (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Messages: %w", err)
		}

		t.Messages = c

	}
	// t.t.BLSAggregate (types.Signature)

	{

		if err := t.BLSAggregate.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Timestamp (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Timestamp = extra
	// t.t.BlockSig (types.Signature)

	{

		if err := t.BlockSig.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	return nil
}

func (t *Ticket) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{129}); err != nil {
		return err
	}

	// t.t.VRFProof ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.VRFProof)))); err != nil {
		return err
	}
	if _, err := w.Write(t.VRFProof); err != nil {
		return err
	}
	return nil
}

func (t *Ticket) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.VRFProof ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.VRFProof: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.VRFProof = make([]byte, extra)
	if _, err := io.ReadFull(br, t.VRFProof); err != nil {
		return err
	}
	return nil
}

func (t *Message) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{136}); err != nil {
		return err
	}

	// t.t.To (address.Address)
	if err := t.To.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.From (address.Address)
	if err := t.From.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Nonce (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Nonce)); err != nil {
		return err
	}

	// t.t.Value (types.BigInt)
	if err := t.Value.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.GasPrice (types.BigInt)
	if err := t.GasPrice.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.GasLimit (types.BigInt)
	if err := t.GasLimit.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Method (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Method)); err != nil {
		return err
	}

	// t.t.Params ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.Params)))); err != nil {
		return err
	}
	if _, err := w.Write(t.Params); err != nil {
		return err
	}
	return nil
}

func (t *Message) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 8 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.To (address.Address)

	{

		if err := t.To.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.From (address.Address)

	{

		if err := t.From.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Nonce (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Nonce = extra
	// t.t.Value (types.BigInt)

	{

		if err := t.Value.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.GasPrice (types.BigInt)

	{

		if err := t.GasPrice.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.GasLimit (types.BigInt)

	{

		if err := t.GasLimit.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Method (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Method = extra
	// t.t.Params ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Params: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.Params = make([]byte, extra)
	if _, err := io.ReadFull(br, t.Params); err != nil {
		return err
	}
	return nil
}

func (t *SignedMessage) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{130}); err != nil {
		return err
	}

	// t.t.Message (types.Message)
	if err := t.Message.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Signature (types.Signature)
	if err := t.Signature.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *SignedMessage) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Message (types.Message)

	{

		if err := t.Message.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Signature (types.Signature)

	{

		if err := t.Signature.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	return nil
}

func (t *MsgMeta) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{130}); err != nil {
		return err
	}

	// t.t.BlsMessages (cid.Cid)

	if err := cbg.WriteCid(w, t.BlsMessages); err != nil {
		return xerrors.Errorf("failed to write cid field t.BlsMessages: %w", err)
	}

	// t.t.SecpkMessages (cid.Cid)

	if err := cbg.WriteCid(w, t.SecpkMessages); err != nil {
		return xerrors.Errorf("failed to write cid field t.SecpkMessages: %w", err)
	}

	return nil
}

func (t *MsgMeta) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.BlsMessages (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.BlsMessages: %w", err)
		}

		t.BlsMessages = c

	}
	// t.t.SecpkMessages (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.SecpkMessages: %w", err)
		}

		t.SecpkMessages = c

	}
	return nil
}

func (t *SignedVoucher) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{137}); err != nil {
		return err
	}

	// t.t.TimeLock (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.TimeLock)); err != nil {
		return err
	}

	// t.t.SecretPreimage ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.SecretPreimage)))); err != nil {
		return err
	}
	if _, err := w.Write(t.SecretPreimage); err != nil {
		return err
	}

	// t.t.Extra (types.ModVerifyParams)
	if err := t.Extra.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Lane (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Lane)); err != nil {
		return err
	}

	// t.t.Nonce (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Nonce)); err != nil {
		return err
	}

	// t.t.Amount (types.BigInt)
	if err := t.Amount.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.MinCloseHeight (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.MinCloseHeight)); err != nil {
		return err
	}

	// t.t.Merges ([]types.Merge)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.Merges)))); err != nil {
		return err
	}
	for _, v := range t.Merges {
		if err := v.MarshalCBOR(w); err != nil {
			return err
		}
	}

	// t.t.Signature (types.Signature)
	if err := t.Signature.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *SignedVoucher) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 9 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.TimeLock (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.TimeLock = extra
	// t.t.SecretPreimage ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.SecretPreimage: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.SecretPreimage = make([]byte, extra)
	if _, err := io.ReadFull(br, t.SecretPreimage); err != nil {
		return err
	}
	// t.t.Extra (types.ModVerifyParams)

	{

		pb, err := br.PeekByte()
		if err != nil {
			return err
		}
		if pb == cbg.CborNull[0] {
			var nbuf [1]byte
			if _, err := br.Read(nbuf[:]); err != nil {
				return err
			}
		} else {
			t.Extra = new(ModVerifyParams)
			if err := t.Extra.UnmarshalCBOR(br); err != nil {
				return err
			}
		}

	}
	// t.t.Lane (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Lane = extra
	// t.t.Nonce (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Nonce = extra
	// t.t.Amount (types.BigInt)

	{

		if err := t.Amount.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.MinCloseHeight (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.MinCloseHeight = extra
	// t.t.Merges ([]types.Merge)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Merges: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}
	if extra > 0 {
		t.Merges = make([]Merge, extra)
	}
	for i := 0; i < int(extra); i++ {

		var v Merge
		if err := v.UnmarshalCBOR(br); err != nil {
			return err
		}

		t.Merges[i] = v
	}

	// t.t.Signature (types.Signature)

	{

		pb, err := br.PeekByte()
		if err != nil {
			return err
		}
		if pb == cbg.CborNull[0] {
			var nbuf [1]byte
			if _, err := br.Read(nbuf[:]); err != nil {
				return err
			}
		} else {
			t.Signature = new(Signature)
			if err := t.Signature.UnmarshalCBOR(br); err != nil {
				return err
			}
		}

	}
	return nil
}

func (t *ModVerifyParams) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{131}); err != nil {
		return err
	}

	// t.t.Actor (address.Address)
	if err := t.Actor.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.Method (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Method)); err != nil {
		return err
	}

	// t.t.Data ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.Data)))); err != nil {
		return err
	}
	if _, err := w.Write(t.Data); err != nil {
		return err
	}
	return nil
}

func (t *ModVerifyParams) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Actor (address.Address)

	{

		if err := t.Actor.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	// t.t.Method (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Method = extra
	// t.t.Data ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Data: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.Data = make([]byte, extra)
	if _, err := io.ReadFull(br, t.Data); err != nil {
		return err
	}
	return nil
}

func (t *Merge) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{130}); err != nil {
		return err
	}

	// t.t.Lane (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Lane)); err != nil {
		return err
	}

	// t.t.Nonce (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Nonce)); err != nil {
		return err
	}
	return nil
}

func (t *Merge) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Lane (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Lane = extra
	// t.t.Nonce (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Nonce = extra
	return nil
}

func (t *Actor) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{132}); err != nil {
		return err
	}

	// t.t.Code (cid.Cid)

	if err := cbg.WriteCid(w, t.Code); err != nil {
		return xerrors.Errorf("failed to write cid field t.Code: %w", err)
	}

	// t.t.Head (cid.Cid)

	if err := cbg.WriteCid(w, t.Head); err != nil {
		return xerrors.Errorf("failed to write cid field t.Head: %w", err)
	}

	// t.t.Nonce (uint64)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, t.Nonce)); err != nil {
		return err
	}

	// t.t.Balance (types.BigInt)
	if err := t.Balance.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *Actor) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 4 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Code (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Code: %w", err)
		}

		t.Code = c

	}
	// t.t.Head (cid.Cid)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Head: %w", err)
		}

		t.Head = c

	}
	// t.t.Nonce (uint64)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint64 field")
	}
	t.Nonce = extra
	// t.t.Balance (types.BigInt)

	{

		if err := t.Balance.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	return nil
}

func (t *MessageReceipt) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{131}); err != nil {
		return err
	}

	// t.t.ExitCode (uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, uint64(t.ExitCode))); err != nil {
		return err
	}

	// t.t.Return ([]uint8)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajByteString, uint64(len(t.Return)))); err != nil {
		return err
	}
	if _, err := w.Write(t.Return); err != nil {
		return err
	}

	// t.t.GasUsed (types.BigInt)
	if err := t.GasUsed.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *MessageReceipt) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.ExitCode (uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint8 field")
	}
	if extra > math.MaxUint8 {
		return fmt.Errorf("integer in input was too large for uint8 field")
	}
	t.ExitCode = uint8(extra)
	// t.t.Return ([]uint8)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.Return: array too large (%d)", extra)
	}

	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}
	t.Return = make([]byte, extra)
	if _, err := io.ReadFull(br, t.Return); err != nil {
		return err
	}
	// t.t.GasUsed (types.BigInt)

	{

		if err := t.GasUsed.UnmarshalCBOR(br); err != nil {
			return err
		}

	}
	return nil
}

func (t *BlockMsg) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{131}); err != nil {
		return err
	}

	// t.t.Header (types.BlockHeader)
	if err := t.Header.MarshalCBOR(w); err != nil {
		return err
	}

	// t.t.BlsMessages ([]cid.Cid)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.BlsMessages)))); err != nil {
		return err
	}
	for _, v := range t.BlsMessages {
		if err := cbg.WriteCid(w, v); err != nil {
			return xerrors.Errorf("failed writing cid field t.BlsMessages: %w", err)
		}
	}

	// t.t.SecpkMessages ([]cid.Cid)
	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.SecpkMessages)))); err != nil {
		return err
	}
	for _, v := range t.SecpkMessages {
		if err := cbg.WriteCid(w, v); err != nil {
			return xerrors.Errorf("failed writing cid field t.SecpkMessages: %w", err)
		}
	}
	return nil
}

func (t *BlockMsg) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.t.Header (types.BlockHeader)

	{

		pb, err := br.PeekByte()
		if err != nil {
			return err
		}
		if pb == cbg.CborNull[0] {
			var nbuf [1]byte
			if _, err := br.Read(nbuf[:]); err != nil {
				return err
			}
		} else {
			t.Header = new(BlockHeader)
			if err := t.Header.UnmarshalCBOR(br); err != nil {
				return err
			}
		}

	}
	// t.t.BlsMessages ([]cid.Cid)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.BlsMessages: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}
	if extra > 0 {
		t.BlsMessages = make([]cid.Cid, extra)
	}
	for i := 0; i < int(extra); i++ {

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("reading cid field t.BlsMessages failed: %w", err)
		}
		t.BlsMessages[i] = c
	}

	// t.t.SecpkMessages ([]cid.Cid)

	maj, extra, err = cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if extra > 8192 {
		return fmt.Errorf("t.SecpkMessages: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}
	if extra > 0 {
		t.SecpkMessages = make([]cid.Cid, extra)
	}
	for i := 0; i < int(extra); i++ {

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("reading cid field t.SecpkMessages failed: %w", err)
		}
		t.SecpkMessages[i] = c
	}

	return nil
}
