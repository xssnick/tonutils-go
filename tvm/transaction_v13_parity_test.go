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
	seed, err := transactionEmulationSeed(blockSeed, tonopsTestAddr)
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

	next, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusActive, false, &msg, false, tlb.BlockchainConfig{})
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

	_, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, &msg, false, tlb.BlockchainConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
		t.Fatalf("public masterchain deploy libs should be bad_state, used=%t skip=%v", used, skip)
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

	_, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, &msg, false, tlb.BlockchainConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
		t.Fatalf("fixed prefix above gv13 limit should be bad_state, used=%t skip=%v", used, skip)
	}
}

func TestTransactionV13PrecompiledGasLoadedFromConfig(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA3, 8).EndCell()
	cfg := tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, 7),
		}),
	}
	gas := vmcore.Gas{Max: 100, Limit: 10, Base: 10, Remaining: 10}

	nextGas, usage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{}, cfg, code, gas)
	if err != nil {
		t.Fatal(err)
	}
	if skip != nil {
		t.Fatalf("unexpected skip reason: %v", skip)
	}
	if usage == nil || usage.Uint64() != 7 {
		t.Fatalf("precompiled usage = %v, want 7", usage)
	}
	if nextGas.Limit != 100 || nextGas.Max != 100 {
		t.Fatalf("precompiled fallback gas = %+v, want max/limit 100", nextGas)
	}
}

func TestTransactionV13PrecompiledGasAboveLimitSkipsCompute(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA4, 8).EndCell()
	cfg := tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, 11),
		}),
	}
	gas := vmcore.Gas{Max: 100, Limit: 10, Base: 10, Remaining: 10}

	_, usage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{}, cfg, code, gas)
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.Uint64() != 11 {
		t.Fatalf("precompiled usage = %v, want 11", usage)
	}
	if skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("skip = %v, want no_gas", skip)
	}
}

func TestTransactionV13MalformedRelaxedCurrencyIsNotSkippedByIgnoreErrors(t *testing.T) {
	badMsg := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(tonopsTestAddr).
		MustStoreUInt(1, 4).
		MustStoreUInt(0, 8).
		EndCell()
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

	actionRes, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), tlb.BlockchainConfig{}, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if actionRes.phase == nil || actionRes.phase.Success || actionRes.phase.ResultCode != 34 || actionRes.phase.SkippedActions != 0 {
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

	actionRes, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), tlb.BlockchainConfig{}, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
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
