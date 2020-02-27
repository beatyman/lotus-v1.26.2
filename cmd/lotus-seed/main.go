package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	badger "github.com/ipfs/go-ds-badger2"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"
	"gopkg.in/urfave/cli.v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/abi/big"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cmd/lotus-seed/seed"
	"github.com/filecoin-project/lotus/genesis"
)

var log = logging.Logger("lotus-seed")

func main() {
	logging.SetLogLevel("*", "INFO")

	local := []*cli.Command{
		genesisCmd,

		preSealCmd,
		aggregateManifestsCmd,
		aggregateSectorDirsCmd,
	}

	app := &cli.App{
		Name:    "lotus-seed",
		Usage:   "Seal sectors for genesis miner",
		Version: build.UserVersion,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "sectorbuilder-dir",
				Value: "~/.genesis-sectors",
			},
		},

		Commands: local,
	}

	if err := app.Run(os.Args); err != nil {
		log.Warn(err)
		os.Exit(1)
	}
}

var preSealCmd = &cli.Command{
	Name: "pre-seal",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "miner-addr",
			Value: "t01000",
			Usage: "specify the future address of your miner",
		},
		&cli.Uint64Flag{
			Name:  "sector-size",
			Value: uint64(build.SectorSizes[0]),
			Usage: "specify size of sectors to pre-seal",
		},
		&cli.StringFlag{
			Name:  "ticket-preimage",
			Value: "lotus is fire",
			Usage: "set the ticket preimage for sealing randomness",
		},
		&cli.IntFlag{
			Name:  "num-sectors",
			Value: 1,
			Usage: "select number of sectors to pre-seal",
		},
		&cli.Uint64Flag{
			Name:  "sector-offset",
			Value: 0,
			Usage: "how many sector ids to skip when starting to seal",
		},
		&cli.StringFlag{
			Name:  "key",
			Value: "",
			Usage: "(optional) Key to use for signing / owner/worker addresses",
		},
	},
	Action: func(c *cli.Context) error {
		sdir := c.String("sectorbuilder-dir")
		sbroot, err := homedir.Expand(sdir)
		if err != nil {
			return err
		}

		maddr, err := address.NewFromString(c.String("miner-addr"))
		if err != nil {
			return err
		}

		var k *types.KeyInfo
		if c.String("key") != "" {
			k = new(types.KeyInfo)
			kh, err := ioutil.ReadFile(c.String("key"))
			if err != nil {
				return err
			}
			kb, err := hex.DecodeString(string(kh))
			if err := json.Unmarshal(kb, k); err != nil {
				return err
			}
		}

		rp, err := registeredProofFromSsize(c.Uint64("sector-size"))
		if err != nil {
			return err
		}

		gm, key, err := seed.PreSeal(maddr, rp, abi.SectorNumber(c.Uint64("sector-offset")), c.Int("num-sectors"), sbroot, []byte(c.String("ticket-preimage")), k)
		if err != nil {
			return err
		}

		return seed.WriteGenesisMiner(maddr, sbroot, gm, key)
	},
}

var aggregateManifestsCmd = &cli.Command{
	Name:  "aggregate-manifests",
	Usage: "aggregate a set of preseal manifests into a single file",
	Action: func(cctx *cli.Context) error {
		var inputs []map[string]genesis.Miner
		for _, infi := range cctx.Args().Slice() {
			fi, err := os.Open(infi)
			if err != nil {
				return err
			}
			defer fi.Close()
			var val map[string]genesis.Miner
			if err := json.NewDecoder(fi).Decode(&val); err != nil {
				return err
			}

			inputs = append(inputs, val)
		}

		output := make(map[string]genesis.Miner)
		for _, in := range inputs {
			for maddr, val := range in {
				if gm, ok := output[maddr]; ok {
					output[maddr] = mergeGenMiners(gm, val)
				} else {
					output[maddr] = val
				}
			}
		}

		blob, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(blob))
		return nil
	},
}

var aggregateSectorDirsCmd = &cli.Command{
	Name:  "aggregate-sector-dirs",
	Usage: "aggregate a set of preseal manifests into a single file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "miner",
			Usage: "Specify address of miner to aggregate sectorbuilders for",
		},
		&cli.StringFlag{
			Name:  "dest",
			Usage: "specify directory to create aggregate sector store in",
		},
		&cli.Uint64Flag{
			Name:  "sector-size",
			Usage: "specify size of sectors to aggregate",
			Value: 32 * 1024 * 1024 * 1024,
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.String("miner") == "" {
			return fmt.Errorf("must specify miner address with --miner")
		}
		if cctx.String("dest") == "" {
			return fmt.Errorf("must specify dest directory with --dest")
		}

		maddr, err := address.NewFromString(cctx.String("miner"))
		if err != nil {
			return err
		}

		destdir, err := homedir.Expand(cctx.String("dest"))
		if err != nil {
			return err
		}

		if err := os.MkdirAll(destdir, 0755); err != nil {
			return err
		}

		agmds, err := badger.NewDatastore(filepath.Join(destdir, "badger"), nil)
		if err != nil {
			return err
		}
		defer agmds.Close()

		ssize := abi.SectorSize(cctx.Uint64("sector-size"))

		rp, err := registeredProofFromSsize(cctx.Uint64("sector-size"))
		if err != nil {
			return err
		}

		sp, err := rp.RegisteredSealProof()
		if err != nil {
			return err
		}

		agsb, err := sectorbuilder.New(&sectorbuilder.Config{
			Miner:         maddr,
			SealProofType: sp,
			PoStProofType: rp,
			Paths:         sectorbuilder.SimplePath(destdir),
			WorkerThreads: 2,
		}, namespace.Wrap(agmds, datastore.NewKey("/sectorbuilder")))
		if err != nil {
			return err
		}

		var aggrGenMiner genesis.Miner
		var highestSectorID abi.SectorNumber
		for _, dir := range cctx.Args().Slice() {
			dir, err := homedir.Expand(dir)
			if err != nil {
				return xerrors.Errorf("failed to expand %q: %w", dir, err)
			}

			st, err := os.Stat(dir)
			if err != nil {
				return err
			}
			if !st.IsDir() {
				return fmt.Errorf("%q was not a directory", dir)
			}

			fi, err := os.Open(filepath.Join(dir, "pre-seal-"+maddr.String()+".json"))
			if err != nil {
				return err
			}

			var genmm map[string]genesis.Miner
			if err := json.NewDecoder(fi).Decode(&genmm); err != nil {
				return err
			}

			genm, ok := genmm[maddr.String()]
			if !ok {
				return xerrors.Errorf("input data did not have our miner in it (%s)", maddr)
			}

			if genm.SectorSize != ssize {
				return xerrors.Errorf("sector size mismatch in %q (%d != %d)", dir)
			}

			for _, s := range genm.Sectors {
				if s.SectorID > highestSectorID {
					highestSectorID = s.SectorID
				}
			}

			aggrGenMiner = mergeGenMiners(aggrGenMiner, genm)

			opts := badger.DefaultOptions
			opts.ReadOnly = true
			mds, err := badger.NewDatastore(filepath.Join(dir, "badger"), &opts)
			if err != nil {
				return err
			}
			defer mds.Close()

			sb, err := sectorbuilder.New(&sectorbuilder.Config{
				Miner:         maddr,
				SealProofType: sp,
				PoStProofType: rp,
				Paths:         sectorbuilder.SimplePath(dir),
				WorkerThreads: 2,
			}, namespace.Wrap(mds, datastore.NewKey("/sectorbuilder")))
			if err != nil {
				return err
			}

			if err := agsb.ImportFrom(sb, false); err != nil {
				return xerrors.Errorf("importing sectors from %q failed: %w", dir, err)
			}
		}

		if err := agsb.SetLastSectorNum(highestSectorID); err != nil {
			return err
		}

		if err := seed.WriteGenesisMiner(maddr, destdir, &aggrGenMiner, nil); err != nil {
			return err
		}

		return nil
	},
}

func mergeGenMiners(a, b genesis.Miner) genesis.Miner {
	if a.SectorSize != b.SectorSize {
		panic("sector sizes mismatch")
	}

	return genesis.Miner{
		Owner:         a.Owner,
		Worker:        a.Worker,
		PeerId:        "",
		MarketBalance: big.Zero(),
		PowerBalance:  big.Zero(),
		SectorSize:    a.SectorSize,
		Sectors:       append(a.Sectors, b.Sectors...),
	}
}

func registeredProofFromSsize(ssize uint64) (abi.RegisteredProof, error) {
	// TODO: this should be provided to us by something lower down...
	switch ssize {
	case 2 << 10:
		return abi.RegisteredProof_StackedDRG2KiBPoSt, nil
	case 32 << 30:
		return abi.RegisteredProof_StackedDRG32GiBPoSt, nil
	case 8 << 20:
		return abi.RegisteredProof_StackedDRG8MiBPoSt, nil
	case 512 << 20:
		return abi.RegisteredProof_StackedDRG512MiBPoSt, nil
	default:
		return 0, fmt.Errorf("unsupported sector size: %d", ssize)
	}
}
