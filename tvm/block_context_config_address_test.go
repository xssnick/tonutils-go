package tvm

import (
	"bytes"
	"math/big"
	"slices"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func blockConfigAddressTestConfig(t *testing.T, param0 *[32]byte, fundamentals ...[32]byte) *PreparedBlockchainConfig {
	t.Helper()

	params := transactionReportStrictConfigParams(t)
	if param0 != nil {
		params[tlb.ConfigParamConfigAddress] = cell.BeginCell().MustStoreSlice(param0[:], 256).EndCell()
	}
	dict := cell.NewDict(256)
	for _, addr := range fundamentals {
		if err := dict.Set(cell.BeginCell().MustStoreSlice(addr[:], 256).EndCell(), cell.BeginCell().EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	fundamental, err := tlb.ToCell(&tlb.FundamentalSmartContractAddresses{Addresses: dict})
	if err != nil {
		t.Fatal(err)
	}
	params[tlb.ConfigParamFundamentalSMCAddresses] = fundamental
	gas, err := (&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		GasCredit:               10_000,
		SpecialGasLimit:         1_000_000,
		BlockGasLimit:           1_000_000,
	}).ToCell()
	if err != nil {
		t.Fatal(err)
	}
	params[tlb.ConfigParamGasPricesMasterchain] = gas
	prepared, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, params))
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestBlockContextConfigAddress(t *testing.T) {
	actual, proposed, fundamental := [32]byte{0xa1}, [32]byte{0xb2}, [32]byte{0xc3}
	for _, tc := range []struct {
		name                string
		param0              *[32]byte
		proposedFundamental bool
	}{
		{name: "missing parameter 0"},
		{name: "proposed address in parameter 0", param0: &proposed},
		{name: "proposed address independently fundamental", param0: &proposed, proposedFundamental: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fundamentals := [][32]byte{fundamental}
			if tc.proposedFundamental {
				fundamentals = append(fundamentals, proposed)
			}
			cfg := blockConfigAddressTestConfig(t, tc.param0, fundamentals...)
			root, rootBOC := cfg.Root(), cfg.Root().ToBOC()
			specials := slices.Clone(cfg.SpecialAccounts())
			opts := BlockOptions{Now: 100, RandSeed: make([]byte, 32)}
			inferred, err := cfg.NewBlockContext(opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.ConfigAddress = &actual
			block, err := cfg.NewBlockContext(opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				addr           [32]byte
				want, inferred bool
			}{
				{actual, true, false},
				{proposed, tc.proposedFundamental, tc.param0 != nil},
				{fundamental, true, true},
				{[32]byte{0xdd}, false, false},
			} {
				addr := address.NewAddress(0, 0xff, check.addr[:])
				if got := block.isSpecialAccount(addr); got != check.want {
					t.Fatalf("actual address override: %x special = %v, want %v", check.addr, got, check.want)
				}
				if got := inferred.isSpecialAccount(addr); got != check.inferred {
					t.Fatalf("nil override: %x special = %v, want %v", check.addr, got, check.inferred)
				}
				if block.isSpecialAccount(address.NewAddress(0, 0, check.addr[:])) {
					t.Fatalf("basechain account %x is special", check.addr)
				}
			}
			if block.Config() != cfg || cfg.Root() != root || !bytes.Equal(root.ToBOC(), rootBOC) {
				t.Fatal("block override changed the prepared config root")
			}
			if !slices.Equal(cfg.SpecialAccounts(), specials) {
				t.Fatal("block override changed public inferred special accounts")
			}
			gotAddr, ok := cfg.ConfigAddress()
			if ok != (tc.param0 != nil) || ok && gotAddr != *tc.param0 {
				t.Fatal("block override changed the parameter-0 accessor")
			}
			gotTuple, gotPresent := block.UnpackedConfig()
			wantTuple, wantPresent := inferred.UnpackedConfig()
			if gotPresent != wantPresent || gotTuple.Len() != wantTuple.Len() {
				t.Fatal("block override changed the unpacked config shape")
			}
			for i := range gotTuple.Len() {
				got, err := gotTuple.RawIndex(i)
				if err != nil {
					t.Fatal(err)
				}
				want, err := wantTuple.RawIndex(i)
				if err != nil {
					t.Fatal(err)
				}
				if got == nil || want == nil {
					if got != want {
						t.Fatalf("unpacked config slot %d changed nullness", i)
					}
					continue
				}
				gotCell, err := got.(*cell.Slice).ToCell()
				if err != nil {
					t.Fatal(err)
				}
				wantCell, err := want.(*cell.Slice).ToCell()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(gotCell.Hash(), wantCell.Hash()) {
					t.Fatalf("unpacked config slot %d changed", i)
				}
			}
		})
	}
}

func TestBlockContextConfigAddressOwnership(t *testing.T) {
	proposed := [32]byte{0xb2}
	cfg := blockConfigAddressTestConfig(t, &proposed)
	actual := [32]byte{}
	block, err := cfg.NewBlockContext(BlockOptions{Now: 100, RandSeed: make([]byte, 32), ConfigAddress: &actual})
	if err != nil {
		t.Fatal(err)
	}
	actual = proposed
	zero := [32]byte{}
	if !block.isSpecialAccount(address.NewAddress(0, 0xff, zero[:])) {
		t.Fatal("explicit zero config address was lost after caller mutation")
	}
	if block.isSpecialAccount(address.NewAddress(0, 0xff, proposed[:])) {
		t.Fatal("block retained the caller's config address pointer")
	}
}

func TestBlockContextConfigAddressExecution(t *testing.T) {
	actual, proposed := [32]byte{0xa1}, [32]byte{0xb2}
	data := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	updated := cell.BeginCell().MustStoreUInt(2, 8).EndCell()
	code := codeFromBuilders(t, funcsop.ACCEPT().Serialize(), stackop.PUSHREF(updated).Serialize(), execop.POPCTR(4).Serialize())
	// One nanogram buys too little gas for ACCEPT unless the account is special.
	storage := tlb.AccountStorage{
		Status:    tlb.AccountStatusActive,
		Balance:   tlb.FromNanoTONU(1),
		StateInit: &tlb.StateInit{Code: code, Data: data, TickTock: &tlb.TickTock{Tick: true, Tock: true}},
	}
	storageCell, err := tlb.ToCell(&storage)
	if err != nil {
		t.Fatal(err)
	}
	usage, _, err := transactionComputeAccountStorageStat(storageCell, 0)
	if err != nil {
		t.Fatal(err)
	}
	storageInfo := tlb.StorageInfo{
		StorageUsed:  tlb.StorageUsed{CellsUsed: new(big.Int).SetUint64(usage.cells), BitsUsed: new(big.Int).SetUint64(usage.bits)},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     100,
	}
	for _, tc := range []struct {
		name   string
		param0 *[32]byte
	}{
		{name: "missing parameter 0"},
		{name: "proposed address in parameter 0", param0: &proposed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := blockConfigAddressTestConfig(t, tc.param0)
			for _, override := range []bool{false, true} {
				opts := BlockOptions{Now: 100, BlockLT: 1, RandSeed: make([]byte, 32)}
				if override {
					opts.ConfigAddress = &actual
				}
				block, err := cfg.NewBlockContext(opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, accountID := range [][32]byte{actual, proposed} {
					want := override && accountID == actual || !override && tc.param0 != nil && accountID == proposed
					addr := address.NewAddress(0, 0xff, accountID[:])
					shard := buildTransactionTestStoredShardAccount(t, addr, storage.Status, 1, storageInfo, storage.StateInit, nil)
					acc, err := PrepareAccount(shard, addr)
					if err != nil {
						t.Fatal(err)
					}
					message, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: addr, Body: cell.BeginCell().EndCell()})
					if err != nil {
						t.Fatal(err)
					}
					msg, err := PrepareMessage(message)
					if err != nil {
						t.Fatal(err)
					}
					txOpts := TransactionOptions{LogicalTime: 1}
					machine := NewTVM()
					accepted, err := machine.CheckExternalMessageAccepted(block, acc, msg, txOpts)
					if err != nil {
						t.Fatal(err)
					}
					if accepted != want {
						t.Fatalf("external precheck override=%v account=%x accepted=%v, want %v", override, accountID, accepted, want)
					}
					result, err := machine.EmulateTransaction(block, acc, msg, txOpts)
					if err != nil {
						t.Fatal(err)
					}
					if result.Accepted != want {
						t.Fatalf("ordinary transaction override=%v account=%x accepted=%v, want %v", override, accountID, result.Accepted, want)
					}
					for _, tock := range []bool{false, true} {
						result, err = machine.EmulateTickTockTransaction(block, acc, tock, txOpts)
						if err != nil {
							t.Fatal(err)
						}
						committed := bytes.Equal(result.NextAccount.State().StateInit.Data.Hash(), updated.Hash())
						if committed != want {
							t.Fatalf("tock=%v override=%v account=%x committed=%v, want %v", tock, override, accountID, committed, want)
						}
					}
				}
			}
		})
	}
}
