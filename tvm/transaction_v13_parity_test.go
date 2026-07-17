package tvm

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTransactionV13RandSeedDerivedFromBlockSeedAndAddress(t *testing.T) {
	blockSeed := bytes.Repeat([]byte{0x11}, 32)
	seed, err := transactionEmulationSeedForTest(blockSeed, tonopsTestAddr, 13)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.New()
	hash.Write(blockSeed)
	hash.Write(tonopsTestAddr.Data())
	want := new(big.Int).SetBytes(hash.Sum(nil))
	if seed.Cmp(want) != 0 {
		t.Fatalf("rand seed mismatch:\n got=%x\nwant=%x", seed.Bytes(), want.Bytes())
	}
}

func TestTransactionV7RandSeedUsesLegacyShiftedAddress(t *testing.T) {
	blockSeed := bytes.Repeat([]byte{0x11}, 32)
	data := append([]byte(nil), tonopsTestAddr.Data()...)
	data[0] &^= 0xE0
	addr := address.NewAddress(0, 0, data).WithAnycast(address.NewAnycast(3, []byte{0xA0}))

	seed, err := transactionEmulationSeedForTest(blockSeed, addr, 7)
	if err != nil {
		t.Fatal(err)
	}

	rewritten := append([]byte(nil), data...)
	rewritten[0] = (rewritten[0] &^ 0xE0) | 0xA0
	hash := sha256.New()
	hash.Write(blockSeed)
	hash.Write(rewritten[:4])
	hash.Write(rewritten[:28])
	want := new(big.Int).SetBytes(hash.Sum(nil))
	if seed.Cmp(want) != 0 {
		t.Fatalf("rand seed mismatch:\n got=%x\nwant=%x", seed.Bytes(), want.Bytes())
	}
}

func TestTransactionRandSeedUsesRewrittenAnycastAddress(t *testing.T) {
	blockSeed := bytes.Repeat([]byte{0x22}, 32)
	data := append([]byte(nil), tonopsTestAddr.Data()...)
	data[0] &^= 0xE0
	addr := address.NewAddress(0, 0, data).WithAnycast(address.NewAnycast(3, []byte{0xA0}))

	seed, err := transactionEmulationSeedForTest(blockSeed, addr, 13)
	if err != nil {
		t.Fatal(err)
	}

	rewritten := append([]byte(nil), data...)
	rewritten[0] = (rewritten[0] &^ 0xE0) | 0xA0
	hash := sha256.New()
	hash.Write(blockSeed)
	hash.Write(rewritten)
	want := new(big.Int).SetBytes(hash.Sum(nil))
	if seed.Cmp(want) != 0 {
		t.Fatalf("rand seed mismatch:\n got=%x\nwant=%x", seed.Bytes(), want.Bytes())
	}

	rawHash := sha256.New()
	rawHash.Write(blockSeed)
	rawHash.Write(data)
	rawSeed := new(big.Int).SetBytes(rawHash.Sum(nil))
	if seed.Cmp(rawSeed) == 0 {
		t.Fatal("rand seed used raw address data instead of rewritten anycast address")
	}
}

func TestTransactionV13InMsgParamsUsesPostStorageMessageValue(t *testing.T) {
	msg := tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			Bounce:    true,
			Bounced:   true,
			SrcAddr:   internalEmulationSrcAddr,
			DstAddr:   tonopsTestAddr,
			Amount:    tlb.FromNanoTONU(777),
			FwdFee:    tlb.FromNanoTONU(5),
			CreatedLT: 123,
			CreatedAt: 456,
		},
	}
	msgBalance, err := transactionCurrencyFromParts(big.NewInt(700), nil)
	if err != nil {
		t.Fatal(err)
	}

	params := transactionBuildInMsgParams(&msg, msgBalance)
	checkInt := func(idx int, want int64) {
		t.Helper()
		got, err := params.Index(idx)
		if err != nil {
			t.Fatal(err)
		}
		n, ok := got.(*big.Int)
		if !ok || n.Int64() != want {
			t.Fatalf("param %d = %#v, want %d", idx, got, want)
		}
	}
	checkInt(0, -1)
	checkInt(1, -1)
	checkInt(3, 5)
	checkInt(4, 123)
	checkInt(5, 456)
	checkInt(6, 777)
	checkInt(7, 700)
}

func TestTransactionV13ActiveAccountUsesInboundStateInitLibraries(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	libs := buildTransactionV13LibraryDict(t, lib, false)
	msg := tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			StateInit: &tlb.StateInit{Lib: libs},
		},
	}
	acc := &transactionRuntimeAccount{
		addr:   tonopsTestAddr,
		status: tlb.AccountStatusActive,
		code:   cell.BeginCell().EndCell(),
		data:   cell.BeginCell().EndCell(),
	}

	next, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusActive, false, &msg, false, emptyPreparedTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if used || skip != nil {
		t.Fatalf("active account should not activate from inbound state, used=%t skip=%v", used, skip)
	}
	if next.inMsgLibraries == nil || !bytes.Equal(next.inMsgLibraries.AsCell().Hash(), libs.AsCell().Hash()) {
		t.Fatal("inbound StateInit libraries were not exposed to VM library resolver")
	}
}

func TestTransactionV13RejectsDeployStateInitPublicLibrariesInMasterchain(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xA2, 8).EndCell()
	libs := buildTransactionV13LibraryDict(t, lib, true)
	stateInit := &tlb.StateInit{
		Code: cell.BeginCell().EndCell(),
		Data: cell.BeginCell().EndCell(),
		Lib:  libs,
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	msg := tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			StateInit: stateInit,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:   address.NewAddress(0, 0xFF, stateCell.Hash()),
		status: tlb.AccountStatusUninit,
	}

	_, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, &msg, false, transactionTestConfigWithGlobalVersion(t, 13))
	if err != nil {
		t.Fatal(err)
	}
	if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
		t.Fatalf("public masterchain deploy libs should be bad_state, used=%t skip=%v", used, skip)
	}
}

func TestTransactionV15RejectsDeployStateInitLibraries(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	libs := buildTransactionV13LibraryDict(t, lib, false)
	stateInit := &tlb.StateInit{
		Code: cell.BeginCell().EndCell(),
		Data: cell.BeginCell().EndCell(),
		Lib:  libs,
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	msg := tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			StateInit: stateInit,
		},
	}

	for _, tt := range []struct {
		name    string
		version uint32
		status  tlb.AccountStatus
		wantBad bool
	}{
		{name: "v14 uninit allows basechain private libs", version: 14, status: tlb.AccountStatusUninit},
		{name: "v15 uninit rejects libs", version: 15, status: tlb.AccountStatusUninit, wantBad: true},
		{name: "v15 nonexist rejects libs", version: 15, status: tlb.AccountStatusNonExist, wantBad: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:   address.NewAddress(0, 0, stateCell.Hash()),
				status: tt.status,
			}

			_, used, skip, err := transactionPrepareComputeAccount(
				acc,
				tt.status,
				false,
				&msg,
				false,
				transactionTestConfigWithGlobalVersion(t, tt.version),
			)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantBad {
				if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
					t.Fatalf("deploy libs should be bad_state, used=%t skip=%v", used, skip)
				}
				return
			}
			if !used || skip != nil {
				t.Fatalf("deploy libs should activate before v15, used=%t skip=%v", used, skip)
			}
		})
	}
}

func TestTransactionV13RejectsDeployStateInitFixedPrefixAboveLimit(t *testing.T) {
	depth := uint64(9)
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  cell.BeginCell().EndCell(),
		Data:  cell.BeginCell().EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	msg := tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			StateInit: stateInit,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:   address.NewAddress(0, 0, stateCell.Hash()),
		status: tlb.AccountStatusUninit,
	}

	_, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, &msg, false, transactionTestConfigWithGlobalVersion(t, 13))
	if err != nil {
		t.Fatal(err)
	}
	if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
		t.Fatalf("fixed prefix above gv13 limit should be bad_state, used=%t skip=%v", used, skip)
	}
}

func TestTransactionV13PrecompiledGasLoadedFromConfig(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA3, 8).EndCell()
	baseGasPrices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1,
		GasLimit:                1_000,
		SpecialGasLimit:         2_000,
		BlockGasLimit:           2_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	masterGasPrices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1,
		GasLimit:                3_000,
		SpecialGasLimit:         4_000,
		BlockGasLimit:           4_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := mustPrepareLenientTestConfig(buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, 7),
		tlb.ConfigParamGasPricesBasechain:   baseGasPrices,
		tlb.ConfigParamGasPricesMasterchain: masterGasPrices,
	}))
	gas := vmcore.Gas{Max: 100, Limit: 10, Base: 10, Remaining: 10}

	for _, tc := range []struct {
		name      string
		addr      *address.Address
		special   bool
		wantLimit int64
	}{
		{name: "ordinary", addr: tonopsTestAddr, wantLimit: 1_000},
		{name: "special", addr: internalEmulationSrcAddr, special: true, wantLimit: 4_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := &transactionExecEnv{}
			nextGas, skip := transactionApplyPrecompiledGasConfig(cfg, code, tc.addr, tc.special, gas, env)
			if skip != nil {
				t.Fatalf("unexpected skip reason: %v", skip)
			}
			if env.precompiledGasUsage == nil || env.precompiledGasUsage.Uint64() != 7 {
				t.Fatalf("precompiled usage = %v, want 7", env.precompiledGasUsage)
			}
			if nextGas.Limit != tc.wantLimit || nextGas.Max != tc.wantLimit {
				t.Fatalf("precompiled fallback gas = %+v, want raw config limit %d", nextGas, tc.wantLimit)
			}
		})
	}
}

func TestTransactionV13PrecompiledGasAboveLimitSkipsCompute(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA4, 8).EndCell()
	cfg := mustPrepareLenientTestConfig(buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, 11),
	}))
	gas := vmcore.Gas{Max: 100, Limit: 10, Base: 10, Remaining: 10}

	env := &transactionExecEnv{}
	_, skip := transactionApplyPrecompiledGasConfig(cfg, code, tonopsTestAddr, false, gas, env)
	if env.precompiledGasUsage == nil || env.precompiledGasUsage.Uint64() != 11 {
		t.Fatalf("precompiled usage = %v, want 11", env.precompiledGasUsage)
	}
	if skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("skip = %v, want no_gas", skip)
	}
}

func TestTransactionHistoricalGasLimitOverride(t *testing.T) {
	prices := &tlb.ConfigGasLimitsPrices{
		FlatGasLimit: 100,
		FlatGasPrice: 100,
		GasPrice:     1,
		GasLimit:     1_000_000,
	}
	addr := address.MustParseRawAddr("0:5E4A5F9DBA638789E6770C990D2959237ACA3BC19D15A734782C26CB19343CC6")
	balance := big.NewInt(10_000_000_000_000)

	for _, tc := range []struct {
		name    string
		version uint32
		now     uint32
		want    uint64
	}{
		{name: "before from version", version: 8, now: 1_710_000_000, want: 1_000_000},
		{name: "from version active", version: 9, now: 1_710_000_000, want: 70_000_000},
		{name: "later version active", version: 14, now: 1_710_000_000, want: 70_000_000},
		{name: "expired", version: 14, now: 1_740_787_200, want: 1_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithGlobalVersion(t, tc.version)
			got := transactionGasBoughtForAccount(cfg, prices, balance, addr, tc.now)
			if got != tc.want {
				t.Fatalf("gas limit override = %d, want %d", got, tc.want)
			}
		})
	}

	firstOverrideAddr := address.MustParseRawAddr("0:FFBFD8F5AE5B2E1C7C3614885CB02145483DFAEE575F0DD08A72C366369211CD")
	if got := transactionGasBoughtForAccount(transactionTestConfigWithGlobalVersion(t, 4), prices, balance, firstOverrideAddr, 1_700_000_000); got != 1_000_000 {
		t.Fatalf("first override before v5 = %d, want default limit", got)
	}
	if got := transactionGasBoughtForAccount(transactionTestConfigWithGlobalVersion(t, 5), prices, balance, firstOverrideAddr, 1_700_000_000); got != 70_000_000 {
		t.Fatalf("first override at v5 = %d, want 70000000", got)
	}
}

func TestTransactionV13NonCanonicalRelaxedCurrencyIsNotSkippedByIgnoreErrors(t *testing.T) {
	badMsg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
		b.MustStoreUInt(4, 4).
			MustStoreSlice([]byte{0x00, 0x0a, 0xb1, 0x47}, 32)
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	})
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 2, Msg: badMsg})
	res := &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      cell.BeginCell().EndCell(),
			Actions:   actions,
			Committed: true,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusActive,
		code:    cell.BeginCell().EndCell(),
		data:    cell.BeginCell().EndCell(),
		balance: big.NewInt(1000),
	}

	actionRes, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), emptyPreparedTestConfig(), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if actionRes.phase == nil || actionRes.phase.Success || !actionRes.phase.Valid || actionRes.phase.ResultCode != 34 || actionRes.phase.SkippedActions != 0 {
		t.Fatalf("unexpected action phase: %+v", actionRes.phase)
	}
}

func TestTransactionV13DeleteAccountRequiresNoReservedBalance(t *testing.T) {
	outMsg := buildTransactionOutboundInternalCell(t, 0)
	actions := buildTransactionActionList(t,
		tlb.ActionReserveCurrency{
			Mode: 0,
			Currency: tlb.CurrencyCollection{
				Coins: tlb.FromNanoTONU(1),
			},
		},
		tlb.ActionSendMsg{Mode: 0xA0, Msg: outMsg},
	)
	res := &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      cell.BeginCell().EndCell(),
			Actions:   actions,
			Committed: true,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusActive,
		code:    cell.BeginCell().EndCell(),
		data:    cell.BeginCell().EndCell(),
		balance: big.NewInt(1000),
	}

	actionRes, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), emptyPreparedTestConfig(), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if actionRes.deleteAccount {
		t.Fatal("delete account should not be requested while reserved balance is non-zero")
	}
	if actionRes.balance.Uint64() != 1 {
		t.Fatalf("remaining balance = %s, want 1", actionRes.balance)
	}
}

func buildTransactionV13LibraryDict(t *testing.T, lib *cell.Cell, public bool) *cell.Dictionary {
	t.Helper()

	libs := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(lib.Hash(), 256).EndCell()
	value := cell.BeginCell().MustStoreBoolBit(public).MustStoreRef(lib).EndCell()
	if err := libs.Set(key, value); err != nil {
		t.Fatalf("store library: %v", err)
	}
	return libs
}

func buildTransactionV13PrecompiledConfig(t *testing.T, code *cell.Cell, gasUsage uint64) *cell.Cell {
	t.Helper()

	smc, err := tlb.ToCell(&tlb.PrecompiledSmc{GasUsage: gasUsage})
	if err != nil {
		t.Fatalf("build precompiled smc: %v", err)
	}
	list := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(code.Hash(), 256).EndCell()
	if err = list.Set(key, smc); err != nil {
		t.Fatalf("store precompiled smc: %v", err)
	}
	cfg, err := tlb.ToCell(&tlb.PrecompiledContractsConfig{List: list})
	if err != nil {
		t.Fatalf("build precompiled config: %v", err)
	}
	return cfg
}
