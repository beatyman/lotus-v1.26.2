package ffiwrapper

import (
	"context"
	"testing"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-storage/storage"
)

func TestCheckProvale(t *testing.T) {
	cases := []struct {
		in       []storage.SectorFile
		in_ssize abi.SectorSize
		out_all  int
		out_good int
		out_bad  int
	}{
		// good
		{
			in: []storage.SectorFile{
				storage.SectorFile{
					SectorId:    "s-t01003-0",
					StorageRepo: "/data/sdb/lotus-user-1/.lotusstorage",
				},
			},
			in_ssize: 2048,
			out_all:  1,
			out_good: 1,
			out_bad:  0,
		},

		// bad path
		{
			in: []storage.SectorFile{
				storage.SectorFile{
					SectorId:    "s-t01003-0",
					StorageRepo: "0",
				},
			},
			in_ssize: 2048,
			out_all:  1,
			out_good: 0,
			out_bad:  1,
		},

		// empty data
		{
			in: []storage.SectorFile{
				storage.SectorFile{},
			},
			in_ssize: 2048,
			out_all:  1,
			out_good: 0,
			out_bad:  1,
		},

		// empty storage
		{
			in: []storage.SectorFile{
				storage.SectorFile{
					SectorId: "s-t030272-1227",
				},
			},
			in_ssize: 2048,
			out_all:  1,
			out_good: 0,
			out_bad:  1,
		},
	}

	ctx := context.TODO()
	for i, c := range cases {
		all, good, bad, err := CheckProvable(ctx, c.in_ssize, c.in, 6*time.Second)
		if err != nil {
			t.Fatal(err, i)
		}
		if len(all) != c.out_all {
			t.Fatal(len(all), c.out_all)
		}
		if len(good) != c.out_good {
			t.Fatal(len(good), c.out_good)
		}
		if len(bad) != c.out_bad {
			t.Fatal(len(bad), c.out_bad)
		}
	}
}
