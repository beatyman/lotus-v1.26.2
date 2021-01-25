package auth

import (
	"testing"
)

func TestHlmAuth(t *testing.T) {
	cases := []struct {
		in []byte // /etc/lotus/auth.dat of lotus

		authAddr string // signer address
		authData []byte // /etc/lotus/auth.dat of miner

		out bool // expect auth
	}{
		// no scope auth set for default
		{
			in: []byte(`
# scope key:scope address
	`),
			authAddr: "address4",
			authData: []byte("dfSlkaajfdfKljd:address3,address4"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
	`),
			authAddr: "any",
			authData: []byte("any"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
	`),
			authAddr: "",
			authData: []byte(""),
			out:      true,
		},

		// normal case
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address3",
			authData: []byte("dfSlkaajfdfKljd:address3,address4"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address4",
			authData: []byte("dfSlkaajfdfKljd:address3,address4"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address3",
			authData: []byte("dfSlkaajfdfKljd"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address3",
			authData: []byte{},
			out:      false,
		},
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address1",
			authData: []byte("abcd:address1,address2"),
			out:      false,
		},
		{
			in: []byte(`
# scope key:scope address
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address1",
			authData: []byte("abcd"),
			out:      false,
		},

		// test root auth
		{
			in: []byte(`
# scope key:scope address
adfadKljadf:*
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address1",
			authData: []byte("adfadKljadf:*"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
adfadKljadf:*
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address2",
			authData: []byte("adfadKljadf:address1"),
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
adfadKljadf:*
lkajdfadfSKljfd:address1,address2
dfSlkaajfdfKljd:address3,address4
	`),
			authAddr: "address2",
			authData: []byte("adfadKljadf"),
			out:      true,
		},

		// test the old version
		{
			in: []byte(`
# scope key:scope address
adfadKljadf
	`),
			authAddr: "any",
			authData: []byte("adfadKljadf"), // default upgrade to root
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
adfadKljadf
	`),
			authAddr: "",
			authData: []byte("adfadKljadf:address1"), // default upgrade to root
			out:      true,
		},
		{
			in: []byte(`
# scope key:scope address
adfadKljadf
	`),
			authAddr: "any",
			authData: []byte(""), // default upgrade to root
			out:      false,
		},
	}

	for index, c := range cases {
		authMap, err := parseHlmAuth(c.in)
		if err != nil {
			t.Fatalf("idx:%d,%s", index, err.Error())
		}
		auths = authMap
		authData = c.in

		if IsHlmAuth(c.authAddr, c.authData) != c.out {
			t.Fatalf("idx:%d,expect %t, but it isn't", index, c.out)
		}
	}
}
