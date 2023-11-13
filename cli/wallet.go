package cli

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/chain/wallet/encode"
	"github.com/filecoin-project/lotus/lib/tablewriter"
	"github.com/gwaylib/errors"
	"github.com/howeyc/gopass"
)

var walletCmd = &cli.Command{
	Name:  "wallet",
	Usage: "Manage wallet",
	Subcommands: []*cli.Command{
		walletGenRootCert,
		walletGenPasswd,
		walletEncode,
		walletChecksum,
		walletNew,
		walletList,
		walletBalance,
		walletExport,
		walletImport,
		walletGetDefault,
		walletSetDefault,
		walletSign,
		walletVerify,
		walletDelete,
		walletMarket,
	},
}
var walletGenRootCert = &cli.Command{
	Name:      "gen-cert",
	Usage:     "generate the encode private cert",
	ArgsUsage: "[output path]",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("must specify path to export")
		}

		privKey, err := encode.GenRsaKey()
		if err != nil {
			return err
		}
		privBytes := x509.MarshalPKCS1PrivateKey(privKey)
		output := []byte(hex.EncodeToString(privBytes))
		return ioutil.WriteFile(cctx.Args().First(), output, 0644)
	},
}
var walletGenPasswd = &cli.Command{
	Name:  "gen-password",
	Usage: "generate password",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "len",
			Value: 96,
			Usage: "length of the password",
		},
	},
	Action: func(cctx *cli.Context) error {
		passwd := encode.RandPlainText(cctx.Int("len"))
		fmt.Println(passwd)
		return nil
	},
}

var walletEncode = &cli.Command{
	Name:      "encode",
	Usage:     "encode the uncrypit key",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() {
			return fmt.Errorf("must specify key to export")
		}

		fmt.Println("Please input password:")
		passwd, err := gopass.GetPasswd()
		if err != nil {
			return err
		}
		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		if err := api.WalletEncode(ctx, addr, string(passwd)); err != nil {
			return err
		}
		fmt.Printf("Encode success, password: %s\n", passwd)
		return nil
	},
}
var walletChecksum = &cli.Command{
	Name:      "checksum",
	Usage:     "checksum the encrypt key",
	ArgsUsage: "[file]",
	Action: func(cctx *cli.Context) error {
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() {
			return fmt.Errorf("must specify key to export")
		}

		inpdata, err := ioutil.ReadFile(cctx.Args().First())
		if err != nil {
			return err
		}
		data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
		if err != nil {
			return err
		}

		var ki types.KeyInfo
		if err := json.Unmarshal(data, &ki); err != nil {
			return err
		}
		if ki.Encrypted {
			fmt.Println("Please input checksum password:")
			passwd, err := gopass.GetPasswd()
			if err != nil {
				return err
			}
			cdata, err := encode.MixDecrypt(ki.PrivateKey, string(passwd))
			if err != nil {
				return err
			}
			ki.PrivateKey = cdata
			ki.Encrypted = false
			fmt.Println("Decode success!")
		} else {
			fmt.Println("WARNNING: the private key not in encrypted!!!!!")
		}
		key, err := key.NewKey(ctx, ki)
		if err != nil {
			return err
		}

		fmt.Printf("checksum address: %s\n", key.Address)
		return nil
	},
}

var walletNew = &cli.Command{
	Name:      "new",
	Usage:     "Generate a new key of the given type",
	ArgsUsage: "[bls|secp256k1 (default secp256k1)]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "encode",
			Value: true,
			Usage: "encode the private key, it will output the password when done",
		},
		&cli.BoolFlag{
			Name:  "local",
			Value: false,
			Usage: "generate private key in local file",
		},
	},
	Action: func(cctx *cli.Context) error {
		t := cctx.Args().First()
		if t == "" {
			t = "secp256k1"
		}
		passwd := ""
		afmt := NewAppFmt(cctx.App)
		if cctx.Bool("encode") {
			afmt.Println("Please input password:")
			pwd, err := gopass.GetPasswd()
			if err != nil {
				return err
			}
			passwd = string(pwd)
		}

		typ := types.KeyType(t)
		if cctx.Bool("local") {
			// by zhoushuyue
			k, err := key.GenerateKey(typ)
			if err != nil {
				return err
			}
			dsName := wallet.KNamePrefix + k.Address.String()
			// encode the private key
			if len(passwd) > 0 {
				eData, err := encode.EncodeData(dsName, k.KeyInfo.PrivateKey, passwd)
				if err != nil {
					return errors.As(err)
				}
				k.KeyInfo.PrivateKey = eData
				k.KeyInfo.DsName = dsName
				k.KeyInfo.Encrypted = true
			}
			keyData, err := json.Marshal(k.KeyInfo)
			if err != nil {
				return xerrors.Errorf("encoding key '%s': %w", dsName, err)
			}

			err = ioutil.WriteFile(k.Address.String()+".dat", []byte(hex.EncodeToString(keyData)), 0600)
			if err != nil {
				return xerrors.Errorf("writing key '%s': %w", dsName, err)
			}
			// end by zhoushuyue

			afmt.Println(k.Address.String())
			return nil
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)
		nk, err := api.WalletNew(ctx, typ, passwd)
		if err != nil {
			return err
		}

		afmt.Println(nk.String())
		return nil
	},
}

var walletList = &cli.Command{
	Name:    "list",
	Aliases: []string{"decode"},
	Usage:   "List wallet address",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "addr-only",
			Usage:   "Only print addresses",
			Aliases: []string{"a"},
		},
		&cli.BoolFlag{
			Name:    "id",
			Usage:   "Output ID addresses",
			Aliases: []string{"i"},
		},
		&cli.BoolFlag{
			Name:    "market",
			Usage:   "Output market balances",
			Aliases: []string{"m"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := ReqContext(cctx)
		api, closer, err := GetFullNodeAPI(cctx)
		afmt := NewAppFmt(cctx.App)
		if err != nil {
			closer = nil

			// local mode
			for {
				time.Sleep(1e9)
				name, sFile, err := encode.InputCryptoUnixStatus(ctx)
				if err != nil {
					if !errors.ErrNoData.Equal(err) {
						return errors.As(err)
					}
					break
				}

				afmt.Printf("Please input password for '%s':\n", name)
				passwd, err := gopass.GetPasswd()
				if err != nil {
					return err
				}
				if err := encode.WriteCryptoUnixPwd(ctx, sFile, string(passwd)); err != nil {
					fmt.Println(err.Error())
					continue
				}
				afmt.Println("Decode success!")
			}
		}
		if closer == nil {
			api, closer, err = GetFullNodeAPI(cctx)
			if err != nil {
				afmt.Println("No local wallets are waiting input, exit.")
				return nil
			}
		}
		defer closer()

		// implement by zhoushuyue
		var addrs []address.Address
		decodeDone := make(chan error, 1)
		go func() {
			addrs, err = api.WalletList(ctx)
			decodeDone <- err
		}()
	input:
		for {
			select {
			case err := <-decodeDone:
				if err != nil {
					return err
				}
				break input
			default:
				time.Sleep(1e9)
				name, err := api.InputWalletStatus(ctx)
				if err != nil {
					return err
				}
				if len(name) == 0 {
					break
				}
				afmt.Printf("Please input password for '%s':\n", name)
				passwd, err := gopass.GetPasswd()
				if err != nil {
					return err
				}
				if err := api.InputWalletPasswd(ctx, string(passwd)); err != nil {
					log.Info(err)
					break
				}
			}
		}

		// end implement by zhoushuyue

		// Assume an error means no default key is set
		def, _ := api.WalletDefaultAddress(ctx)

		tw := tablewriter.New(
			tablewriter.Col("Address"),
			tablewriter.Col("ID"),
			tablewriter.Col("Balance"),
			tablewriter.Col("Market(Avail)"),
			tablewriter.Col("Market(Locked)"),
			tablewriter.Col("Nonce"),
			tablewriter.Col("Default"),
			tablewriter.Col("Encrypt"),
			tablewriter.NewLineCol("Error"))

		for _, addr := range addrs {
			if cctx.Bool("addr-only") {
				afmt.Println(addr.String())
			} else {
				a, err := api.StateGetActor(ctx, addr, types.EmptyTSK)
				if err != nil {
					if !strings.Contains(err.Error(), "actor not found") {
						tw.Write(map[string]interface{}{
							"Address": addr,
							"Error":   err,
						})
						continue
					}

					a = &types.Actor{
						Balance: big.Zero(),
					}
				}

				row := map[string]interface{}{
					"Address": addr,
					"Balance": types.FIL(a.Balance),
					"Nonce":   a.Nonce,
				}
				if addr == def {
					row["Default"] = "X"
				}

				keyInfo, err := api.WalletExport(ctx, addr)
				if err != nil {
					row["Encrypt"] = err.Error()
				} else {
					row["Encrypt"] = keyInfo.Encrypted
				}

				if cctx.Bool("id") {
					id, err := api.StateLookupID(ctx, addr, types.EmptyTSK)
					if err != nil {
						row["ID"] = "n/a"
					} else {
						row["ID"] = id
					}
				}

				if cctx.Bool("market") {
					mbal, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
					if err == nil {
						row["Market(Avail)"] = types.FIL(types.BigSub(mbal.Escrow, mbal.Locked))
						row["Market(Locked)"] = types.FIL(mbal.Locked)
					}
				}

				tw.Write(row)
			}
		}

		if !cctx.Bool("addr-only") {
			return tw.Flush(os.Stdout)
		}

		return nil
	},
}

var walletBalance = &cli.Command{
	Name:      "balance",
	Usage:     "Get account balance",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		var addr address.Address
		if cctx.Args().First() != "" {
			addr, err = address.NewFromString(cctx.Args().First())
		} else {
			addr, err = api.WalletDefaultAddress(ctx)
		}
		if err != nil {
			return err
		}

		balance, err := api.WalletBalance(ctx, addr)
		if err != nil {
			return err
		}

		inSync, err := IsSyncDone(ctx, api)
		if err != nil {
			return err
		}

		if balance.Equals(types.NewInt(0)) && !inSync {
			afmt.Printf("%s (warning: may display 0 if chain sync in progress)\n", types.FIL(balance))
		} else {
			afmt.Printf("%s\n", types.FIL(balance))
		}

		return nil
	},
}

var walletGetDefault = &cli.Command{
	Name:  "default",
	Usage: "Get default wallet address",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		addr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			return err
		}

		afmt.Printf("%s\n", addr.String())
		return nil
	},
}

var walletSetDefault = &cli.Command{
	Name:      "set-default",
	Usage:     "Set default wallet address",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if cctx.NArg() != 1 {
			return IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println("Default address set to:", addr)
		return api.WalletSetDefault(ctx, addr)
	},
}

var walletExport = &cli.Command{
	Name:      "export",
	Usage:     "export keys",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		if cctx.NArg() != 1 {
			return IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		ki, err := api.WalletExport(ctx, addr)
		if err != nil {
			return err
		}

		b, err := json.Marshal(ki)
		if err != nil {
			return err
		}

		afmt.Println(hex.EncodeToString(b))
		return nil
	},
}

var walletImport = &cli.Command{
	Name:      "import",
	Usage:     "import keys",
	ArgsUsage: "[<path> (optional, will read from stdin if omitted)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "specify input format for key",
			Value: "hex-lotus",
		},
		&cli.BoolFlag{
			Name:  "as-default",
			Usage: "import the given key as your new default key",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var inpdata []byte
		if !cctx.Args().Present() || cctx.Args().First() == "-" {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Print("Enter private key(not display in the terminal): ")

				sigCh := make(chan os.Signal, 1)
				// Notify the channel when SIGINT is received
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

				go func() {
					<-sigCh
					fmt.Println("\nInterrupt signal received. Exiting...")
					os.Exit(1)
				}()

				inpdata, err = term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return err
				}
				fmt.Println()
			} else {
				reader := bufio.NewReader(os.Stdin)
				indata, err := reader.ReadBytes('\n')
				if err != nil {
					return err
				}
				inpdata = indata
			}

		} else {
			fdata, err := os.ReadFile(cctx.Args().First())
			if err != nil {
				return err
			}
			inpdata = fdata
		}

		var ki types.KeyInfo
		switch cctx.String("format") {
		case "hex-lotus":
			data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &ki); err != nil {
				return err
			}
		case "json-lotus":
			if err := json.Unmarshal(inpdata, &ki); err != nil {
				return err
			}
		case "gfc-json":
			var f struct {
				KeyInfo []struct {
					PrivateKey []byte
					SigType    int
				}
			}
			if err := json.Unmarshal(inpdata, &f); err != nil {
				return xerrors.Errorf("failed to parse go-filecoin key: %s", err)
			}

			gk := f.KeyInfo[0]
			ki.PrivateKey = gk.PrivateKey
			switch gk.SigType {
			case 1:
				ki.Type = types.KTSecp256k1
			case 2:
				ki.Type = types.KTBLS
			default:
				return fmt.Errorf("unrecognized key type: %d", gk.SigType)
			}
		default:
			return fmt.Errorf("unrecognized format: %s", cctx.String("format"))
		}

		var addr address.Address
		decodeDone := make(chan error, 1)

		go func() {
			addr, err = api.WalletImport(ctx, &ki)
			decodeDone <- err
		}()
	input:
		for {
			select {
			case err := <-decodeDone:
				if err != nil {
					return err
				}
				break input
			default:
				time.Sleep(1e9)
				name, err := api.InputWalletStatus(ctx)
				if err != nil {
					return err
				}
				if len(name) == 0 {
					break
				}
				fmt.Printf("Please input password for '%s':\n", name)
				passwd, err := gopass.GetPasswd()
				if err != nil {
					return err
				}
				if err := api.InputWalletPasswd(ctx, string(passwd)); err != nil {
					log.Info(err)
					break
				}
			}
		}

		if cctx.Bool("as-default") {
			if err := api.WalletSetDefault(ctx, addr); err != nil {
				return fmt.Errorf("failed to set default key: %w", err)
			}
		}

		fmt.Printf("imported key %s successfully!\n", addr)
		return nil
	},
}

var walletSign = &cli.Command{
	Name:      "sign",
	Usage:     "sign a message",
	ArgsUsage: "<signing address> <hexMessage>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		if cctx.NArg() != 2 {
			return IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())

		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))

		if err != nil {
			return err
		}

		sig, err := api.WalletSign(ctx, addr, msg)

		if err != nil {
			return err
		}

		sigBytes := append([]byte{byte(sig.Type)}, sig.Data...)

		afmt.Println(hex.EncodeToString(sigBytes))
		return nil
	},
}

var walletVerify = &cli.Command{
	Name:      "verify",
	Usage:     "verify the signature of a message",
	ArgsUsage: "<signing address> <hexMessage> <signature>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		if cctx.NArg() != 3 {
			return IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())

		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))

		if err != nil {
			return err
		}

		sigBytes, err := hex.DecodeString(cctx.Args().Get(2))

		if err != nil {
			return err
		}

		var sig crypto.Signature
		if err := sig.UnmarshalBinary(sigBytes); err != nil {
			return err
		}

		ok, err := api.WalletVerify(ctx, addr, msg, &sig)
		if err != nil {
			return err
		}
		if ok {
			afmt.Println("valid")
			return nil
		}
		afmt.Println("invalid")
		return NewCliError("CLI Verify called with invalid signature")
	},
}

var walletDelete = &cli.Command{
	Name:      "delete",
	Usage:     "Soft delete an address from the wallet - hard deletion needed for permanent removal",
	ArgsUsage: "<address> ",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if cctx.NArg() != 1 {
			return IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println("Soft deleting address:", addr)
		fmt.Println("Hard deletion of the address in `~/.lotus/keystore` is needed for permanent removal")
		return api.WalletDelete(ctx, addr)
	},
}

var walletMarket = &cli.Command{
	Name:  "market",
	Usage: "Interact with market balances",
	Subcommands: []*cli.Command{
		walletMarketWithdraw,
		walletMarketAdd,
	},
}

var walletMarketWithdraw = &cli.Command{
	Name:      "withdraw",
	Usage:     "Withdraw funds from the Storage Market Actor",
	ArgsUsage: "[amount (FIL) optional, otherwise will withdraw max available]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "wallet",
			Usage:   "Specify address to withdraw funds to, otherwise it will use the default wallet address",
			Aliases: []string{"w"},
		},
		&cli.StringFlag{
			Name:    "address",
			Usage:   "Market address to withdraw from (account or miner actor address, defaults to --wallet address)",
			Aliases: []string{"a"},
		},
		&cli.IntFlag{
			Name:  "confidence",
			Usage: "number of block confirmations to wait for",
			Value: int(build.MessageConfidence),
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting node API: %w", err)
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		var wallet address.Address
		if cctx.String("wallet") != "" {
			wallet, err = address.NewFromString(cctx.String("wallet"))
			if err != nil {
				return xerrors.Errorf("parsing from address: %w", err)
			}
		} else {
			wallet, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return xerrors.Errorf("getting default wallet address: %w", err)
			}
		}

		addr := wallet
		if cctx.String("address") != "" {
			addr, err = address.NewFromString(cctx.String("address"))
			if err != nil {
				return xerrors.Errorf("parsing market address: %w", err)
			}
		}

		// Work out if there are enough unreserved, unlocked funds to withdraw
		bal, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting market balance for address %s: %w", addr.String(), err)
		}

		reserved, err := api.MarketGetReserved(ctx, addr)
		if err != nil {
			return xerrors.Errorf("getting market reserved amount for address %s: %w", addr.String(), err)
		}

		avail := big.Subtract(big.Subtract(bal.Escrow, bal.Locked), reserved)

		notEnoughErr := func(msg string) error {
			return xerrors.Errorf("%s; "+
				"available (%s) = escrow (%s) - locked (%s) - reserved (%s)",
				msg, types.FIL(avail), types.FIL(bal.Escrow), types.FIL(bal.Locked), types.FIL(reserved))
		}

		if avail.IsZero() || avail.LessThan(big.Zero()) {
			avail = big.Zero()
			return notEnoughErr("no funds available to withdraw")
		}

		// Default to withdrawing all available funds
		amt := avail

		// If there was an amount argument, only withdraw that amount
		if cctx.Args().Present() {
			f, err := types.ParseFIL(cctx.Args().First())
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}

			amt = abi.TokenAmount(f)
		}

		// Check the amount is positive
		if amt.IsZero() || amt.LessThan(big.Zero()) {
			return xerrors.Errorf("amount must be > 0")
		}

		// Check there are enough available funds
		if amt.GreaterThan(avail) {
			msg := fmt.Sprintf("can't withdraw more funds than available; requested: %s", types.FIL(amt))
			return notEnoughErr(msg)
		}

		fmt.Printf("Submitting WithdrawBalance message for amount %s for address %s\n", types.FIL(amt), wallet.String())
		smsg, err := api.MarketWithdraw(ctx, wallet, addr, amt)
		if err != nil {
			return xerrors.Errorf("fund manager withdraw error: %w", err)
		}

		afmt.Printf("WithdrawBalance message cid: %s\n", smsg)

		// wait for it to get mined into a block
		wait, err := api.StateWaitMsg(ctx, smsg, uint64(cctx.Int("confidence")))
		if err != nil {
			return err
		}

		// check it executed successfully
		if wait.Receipt.ExitCode.IsError() {
			afmt.Println(cctx.App.Writer, "withdrawal failed!")
			return err
		}

		nv, err := api.StateNetworkVersion(ctx, wait.TipSet)
		if err != nil {
			return err
		}

		if nv >= network.Version14 {
			var withdrawn abi.TokenAmount
			if err := withdrawn.UnmarshalCBOR(bytes.NewReader(wait.Receipt.Return)); err != nil {
				return err
			}

			afmt.Printf("Successfully withdrew %s \n", types.FIL(withdrawn))
			if withdrawn.LessThan(amt) {
				fmt.Printf("Note that this is less than the requested amount of %s \n", types.FIL(amt))
			}
		}

		return nil
	},
}

var walletMarketAdd = &cli.Command{
	Name:      "add",
	Usage:     "Add funds to the Storage Market Actor",
	ArgsUsage: "<amount>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "from",
			Usage:   "Specify address to move funds from, otherwise it will use the default wallet address",
			Aliases: []string{"f"},
		},
		&cli.StringFlag{
			Name:    "address",
			Usage:   "Market address to move funds to (account or miner actor address, defaults to --from address)",
			Aliases: []string{"a"},
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting node API: %w", err)
		}
		defer closer()
		ctx := ReqContext(cctx)

		afmt := NewAppFmt(cctx.App)

		// Get amount param
		if cctx.NArg() < 1 {
			return IncorrectNumArgs(cctx)
		}
		f, err := types.ParseFIL(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("parsing 'amount' argument: %w", err)
		}

		amt := abi.TokenAmount(f)

		// Get from param
		var from address.Address
		if cctx.String("from") != "" {
			from, err = address.NewFromString(cctx.String("from"))
			if err != nil {
				return xerrors.Errorf("parsing from address: %w", err)
			}
		} else {
			from, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return xerrors.Errorf("getting default wallet address: %w", err)
			}
		}

		// Get address param
		addr := from
		if cctx.String("address") != "" {
			addr, err = address.NewFromString(cctx.String("address"))
			if err != nil {
				return xerrors.Errorf("parsing market address: %w", err)
			}
		}

		// Add balance to market actor
		fmt.Printf("Submitting Add Balance message for amount %s for address %s\n", types.FIL(amt), addr)
		smsg, err := api.MarketAddBalance(ctx, from, addr, amt)
		if err != nil {
			return xerrors.Errorf("add balance error: %w", err)
		}

		afmt.Printf("AddBalance message cid: %s\n", smsg)

		return nil
	},
}
