// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package hello

import (
	"fmt"
	"io"

	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf

func (t *Message) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write([]byte{165}); err != nil {
		return err
	}

	// t.HeaviestTipSet ([]cid.Cid) (slice)
	if len("HeaviestTipSet") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"HeaviestTipSet\" was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajTextString, uint64(len("HeaviestTipSet")))); err != nil {
		return err
	}
	if _, err := w.Write([]byte("HeaviestTipSet")); err != nil {
		return err
	}

	if len(t.HeaviestTipSet) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.HeaviestTipSet was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajArray, uint64(len(t.HeaviestTipSet)))); err != nil {
		return err
	}
	for _, v := range t.HeaviestTipSet {
		if err := cbg.WriteCid(w, v); err != nil {
			return xerrors.Errorf("failed writing cid field t.HeaviestTipSet: %w", err)
		}
	}

	// t.HeaviestTipSetWeight (big.Int) (struct)
	if len("HeaviestTipSetWeight") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"HeaviestTipSetWeight\" was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajTextString, uint64(len("HeaviestTipSetWeight")))); err != nil {
		return err
	}
	if _, err := w.Write([]byte("HeaviestTipSetWeight")); err != nil {
		return err
	}

	if err := t.HeaviestTipSetWeight.MarshalCBOR(w); err != nil {
		return err
	}

	// t.GenesisHash (cid.Cid) (struct)
	if len("GenesisHash") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"GenesisHash\" was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajTextString, uint64(len("GenesisHash")))); err != nil {
		return err
	}
	if _, err := w.Write([]byte("GenesisHash")); err != nil {
		return err
	}

	if err := cbg.WriteCid(w, t.GenesisHash); err != nil {
		return xerrors.Errorf("failed to write cid field t.GenesisHash: %w", err)
	}

	// t.TArrial (int64) (int64)
	if len("TArrial") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"TArrial\" was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajTextString, uint64(len("TArrial")))); err != nil {
		return err
	}
	if _, err := w.Write([]byte("TArrial")); err != nil {
		return err
	}

	if t.TArrial >= 0 {
		if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, uint64(t.TArrial))); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajNegativeInt, uint64(-t.TArrial)-1)); err != nil {
			return err
		}
	}

	// t.TSent (int64) (int64)
	if len("TSent") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"TSent\" was too long")
	}

	if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajTextString, uint64(len("TSent")))); err != nil {
		return err
	}
	if _, err := w.Write([]byte("TSent")); err != nil {
		return err
	}

	if t.TSent >= 0 {
		if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajUnsignedInt, uint64(t.TSent))); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(cbg.CborEncodeMajorType(cbg.MajNegativeInt, uint64(-t.TSent)-1)); err != nil {
			return err
		}
	}
	return nil
}

func (t *Message) UnmarshalCBOR(r io.Reader) error {
	br := cbg.GetPeeker(r)

	maj, extra, err := cbg.CborReadHeader(br)
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("Message: map struct too large (%d)", extra)
	}

	var name string
	n := extra

	for i := uint64(0); i < n; i++ {

		{
			sval, err := cbg.ReadString(br)
			if err != nil {
				return err
			}

			name = string(sval)
		}

		switch name {
		// t.HeaviestTipSet ([]cid.Cid) (slice)
		case "HeaviestTipSet":

			maj, extra, err = cbg.CborReadHeader(br)
			if err != nil {
				return err
			}

			if extra > cbg.MaxLength {
				return fmt.Errorf("t.HeaviestTipSet: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}
			if extra > 0 {
				t.HeaviestTipSet = make([]cid.Cid, extra)
			}
			for i := 0; i < int(extra); i++ {

				c, err := cbg.ReadCid(br)
				if err != nil {
					return xerrors.Errorf("reading cid field t.HeaviestTipSet failed: %w", err)
				}
				t.HeaviestTipSet[i] = c
			}

			// t.HeaviestTipSetWeight (big.Int) (struct)
		case "HeaviestTipSetWeight":

			{

				if err := t.HeaviestTipSetWeight.UnmarshalCBOR(br); err != nil {
					return err
				}

			}
			// t.GenesisHash (cid.Cid) (struct)
		case "GenesisHash":

			{

				c, err := cbg.ReadCid(br)
				if err != nil {
					return xerrors.Errorf("failed to read cid field t.GenesisHash: %w", err)
				}

				t.GenesisHash = c

			}
			// t.TArrial (int64) (int64)
		case "TArrial":
			{
				maj, extra, err := cbg.CborReadHeader(br)
				var extraI int64
				if err != nil {
					return err
				}
				switch maj {
				case cbg.MajUnsignedInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 positive overflow")
					}
				case cbg.MajNegativeInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 negative oveflow")
					}
					extraI = -1 - extraI
				default:
					return fmt.Errorf("wrong type for int64 field: %d", maj)
				}

				t.TArrial = int64(extraI)
			}
			// t.TSent (int64) (int64)
		case "TSent":
			{
				maj, extra, err := cbg.CborReadHeader(br)
				var extraI int64
				if err != nil {
					return err
				}
				switch maj {
				case cbg.MajUnsignedInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 positive overflow")
					}
				case cbg.MajNegativeInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 negative oveflow")
					}
					extraI = -1 - extraI
				default:
					return fmt.Errorf("wrong type for int64 field: %d", maj)
				}

				t.TSent = int64(extraI)
			}

		default:
			return fmt.Errorf("unknown struct field %d: '%s'", i, name)
		}
	}

	return nil
}
