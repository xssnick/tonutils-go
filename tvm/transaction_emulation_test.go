package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const transactionTestLogicalTime = int64(1_000_000)

func TestTransactionNormalizeGasUsageCapsOverspend(t *testing.T) {
	res := &MessageExecutionResult{
		ExecutionResult: ExecutionResult{
			ExitCode: ^int64(vmerr.CodeOutOfGas),
			GasUsed:  2963,
			Gas:      vmcore.Gas{Base: 2835},
		},
		Accepted: true,
	}

	transactionNormalizeGasUsage(res)
	if res.GasUsed != 2835 {
		t.Fatalf("gas used = %d, want capped gas limit", res.GasUsed)
	}
}

func TestTransactionPrecompiledGasUsageDoesNotOverrideOutOfGas(t *testing.T) {
	res := &MessageExecutionResult{
		ExecutionResult: ExecutionResult{
			ExitCode: ^int64(vmerr.CodeOutOfGas),
			GasUsed:  2835,
			Steps:    65,
		},
		Accepted: true,
	}

	if err := transactionApplyPrecompiledGasUsage(res, big.NewInt(2963)); err != nil {
		t.Fatal(err)
	}
	if res.GasUsed != 2835 {
		t.Fatalf("gas used = %d, want TVM out-of-gas value", res.GasUsed)
	}
	if res.Steps != 65 {
		t.Fatalf("steps = %d, want original VM steps", res.Steps)
	}
}

func TestTransactionPrecompiledGasUsageOverridesSuccessfulTVM(t *testing.T) {
	res := &MessageExecutionResult{
		ExecutionResult: ExecutionResult{
			ExitCode: 0,
			GasUsed:  120,
			Steps:    65,
		},
		Accepted: true,
	}

	if err := transactionApplyPrecompiledGasUsage(res, big.NewInt(2963)); err != nil {
		t.Fatal(err)
	}
	if res.GasUsed != 2963 {
		t.Fatalf("gas used = %d, want precompiled gas", res.GasUsed)
	}
	if res.Steps != 0 {
		t.Fatalf("steps = %d, want hidden VM steps", res.Steps)
	}
}

func TestTransactionConfigForExternalMessageAcceptedCopiesRuntimeFields(t *testing.T) {
	library := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	configRoot := cell.BeginCell().MustStoreUInt(0xC0FE, 16).EndCell()
	prevBlocks := &struct{ tag uint32 }{tag: 0xAA}
	duePayment := &struct{ tag uint32 }{tag: 0xBB}
	precompiledGasUsage := big.NewInt(2963)
	unpackedConfigValue := big.NewInt(7)
	traceHook := func(vmcore.TraceStep) {}

	cfg := CheckExternalMessageAcceptedConfig{
		Now:                 17,
		BlockLT:             31,
		LogicalTime:         43,
		RandSeed:            []byte{1, 2, 3},
		ConfigRoot:          configRoot,
		PrevBlocks:          prevBlocks,
		UnpackedConfig:      tuple.NewTupleValue(unpackedConfigValue),
		DuePayment:          duePayment,
		PrecompiledGasUsage: precompiledGasUsage,
		Libraries:           []*cell.Cell{library},
		ChksigAlwaysSucceed: true,
		TraceHook:           traceHook,
	}

	got := transactionConfigForExternalMessageAccepted(cfg)
	if got.Now != cfg.Now || got.BlockLT != cfg.BlockLT || got.LogicalTime != cfg.LogicalTime {
		t.Fatalf("time fields were not copied: got %d/%d/%d", got.Now, got.BlockLT, got.LogicalTime)
	}
	if !bytes.Equal(got.RandSeed, cfg.RandSeed) || got.ConfigRoot != cfg.ConfigRoot {
		t.Fatal("random seed or config root was not copied")
	}
	if got.PrevBlocks != cfg.PrevBlocks || got.DuePayment != cfg.DuePayment || got.PrecompiledGasUsage != cfg.PrecompiledGasUsage {
		t.Fatal("transaction context pointers were not copied")
	}
	gotUnpacked, err := got.UnpackedConfig.RawIndex(0)
	if err != nil || got.UnpackedConfig.Len() != 1 || gotUnpacked != unpackedConfigValue {
		t.Fatalf("unpacked config = %v, want %v", got.UnpackedConfig, cfg.UnpackedConfig)
	}
	if len(got.Libraries) != 1 || got.Libraries[0] != cfg.Libraries[0] {
		t.Fatal("libraries were not copied")
	}
	if !got.StopOnAccept {
		t.Fatal("StopOnAccept should be forced for acceptance checks")
	}
	if got.ChksigAlwaysSucceed != cfg.ChksigAlwaysSucceed {
		t.Fatal("ChksigAlwaysSucceed was not copied")
	}
	if got.TraceHook == nil {
		t.Fatal("TraceHook was not copied")
	}
}

func transactionTestLibraryDict(tag uint64) (*cell.Dictionary, *cell.Cell) {
	root := cell.BeginCell().MustStoreUInt(tag, 8).EndCell()
	return root.AsDict(256), root
}

func TestTransactionExecutionLibraries(t *testing.T) {
	cfgLib := cell.BeginCell().MustStoreUInt(0xC7, 8).EndCell()
	cfg := TransactionEmulationConfig{
		Libraries: []*cell.Cell{cfgLib},
	}

	got := transactionExecutionLibraries(&transactionRuntimeAccount{}, cfg, false)
	if len(got) != 1 || got[0] != cfgLib {
		t.Fatalf("libraries without account libs = %v, want cfg library only", got)
	}

	inMsgDict, inMsgRoot := transactionTestLibraryDict(0xA1)
	accountDict, accountRoot := transactionTestLibraryDict(0xA2)
	got = transactionExecutionLibraries(&transactionRuntimeAccount{
		inMsgLibraries: inMsgDict,
		libraries:      accountDict,
	}, cfg, false)
	want := []*cell.Cell{inMsgRoot, accountRoot, cfgLib}
	if len(got) != len(want) {
		t.Fatalf("libraries len = %d, want %d", len(got), len(want))
	}
	for i, lib := range want {
		if got[i] != lib {
			t.Fatalf("libraries[%d] = %p, want %p", i, got[i], lib)
		}
	}
}

func makeTransactionExternalSuccessCode(t *testing.T, newData *cell.Cell) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.ACCEPT().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func TestTransactionStateInitMatchesAddressFixedPrefix(t *testing.T) {
	depth := uint64(8)
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  cell.BeginCell().EndCell(),
		Data:  cell.BeginCell().EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("serialize state init: %v", err)
	}

	addrData := append([]byte(nil), stateCell.Hash()...)
	addrData[0] ^= 0xff
	if !transactionStateInitMatchesAddress(stateCell.Hash(), address.NewAddress(0, 0, addrData), stateInit.Depth) {
		t.Fatal("fixed prefix hash should match when only prefix bits differ")
	}

	addrData[1] ^= 0x80
	if transactionStateInitMatchesAddress(stateCell.Hash(), address.NewAddress(0, 0, addrData), stateInit.Depth) {
		t.Fatal("fixed prefix hash should not match when suffix bits differ")
	}
}

func makeTransactionExternalAcceptThenFailCode(t *testing.T) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.ACCEPT().Serialize(),
		stackop.DROP().Serialize(),
	)
}

func makeTransactionExternalCommitThenThrowCode(t *testing.T, newData, outMsg *cell.Cell, exitCode uint64) *cell.Cell {
	t.Helper()
	return makeTransactionExternalCommitThenThrowCodeWithMode(t, newData, outMsg, 0, exitCode)
}

func makeTransactionExternalCommitThenThrowCodeWithMode(t *testing.T, newData, outMsg *cell.Cell, mode uint8, exitCode uint64) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.ACCEPT().Serialize(),
		stackop.PUSHREF(outMsg).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
		funcsop.COMMIT().Serialize(),
		cell.BeginCell().MustStoreUInt(0xF2C000|exitCode, 24),
	)
}

func makeTransactionInternalSuccessCode(t *testing.T, newData *cell.Cell) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalSendCode(t *testing.T, outMsg, newData *cell.Cell, mode uint8) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(outMsg).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionStackUnderflowCode(t *testing.T) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
	)
}

func buildTransactionTestShardAccount(t *testing.T, addr *address.Address, code, data *cell.Cell, balance uint64, lastPaid uint32) *tlb.ShardAccount {
	t.Helper()

	return buildTransactionTestShardAccountWithStorageInfo(t, addr, code, data, balance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     lastPaid,
	})
}

func buildTransactionTestShardAccountWithStorageInfo(t *testing.T, addr *address.Address, code, data *cell.Cell, balance uint64, storageInfo tlb.StorageInfo) *tlb.ShardAccount {
	t.Helper()

	if storageInfo.StorageExtra == nil {
		storageInfo.StorageExtra = tlb.StorageExtraNone{}
	}

	storageInfoCell, err := tlb.ToCell(&storageInfo)
	if err != nil {
		t.Fatalf("failed to serialize storage info: %v", err)
	}

	stateInitCell, err := tlb.ToCell(&tlb.StateInit{
		Code: code,
		Data: data,
	})
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}

	accountCell := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreUInt(0, 64).
		MustStoreBigCoins(new(big.Int).SetUint64(balance)).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func buildTransactionTestStoredShardAccount(t *testing.T, addr *address.Address, status tlb.AccountStatus, balance uint64, storageInfo tlb.StorageInfo, stateInit *tlb.StateInit, stateHash []byte) *tlb.ShardAccount {
	t.Helper()

	if storageInfo.StorageExtra == nil {
		storageInfo.StorageExtra = tlb.StorageExtraNone{}
	}
	if storageInfo.StorageUsed.CellsUsed == nil {
		storageInfo.StorageUsed.CellsUsed = big.NewInt(0)
	}
	if storageInfo.StorageUsed.BitsUsed == nil {
		storageInfo.StorageUsed.BitsUsed = big.NewInt(0)
	}

	accountCell, err := tlb.ToCell(&tlb.AccountState{
		IsValid:     true,
		Address:     addr,
		StorageInfo: storageInfo,
		AccountStorage: tlb.AccountStorage{
			Status:          status,
			Balance:         tlb.FromNanoTONU(balance),
			StateInit:       stateInit,
			StateHash:       stateHash,
			ExtraCurrencies: nil,
		},
	})
	if err != nil {
		t.Fatalf("failed to build account state: %v", err)
	}

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func buildTransactionTestUninitShardAccount(t *testing.T, addr *address.Address, balance uint64, storageInfo tlb.StorageInfo) *tlb.ShardAccount {
	t.Helper()

	return buildTransactionTestStoredShardAccount(t, addr, tlb.AccountStatusUninit, balance, storageInfo, nil, nil)
}

func mustParseTransactionTestAccount(t *testing.T, shard *tlb.ShardAccount) *tlb.AccountState {
	t.Helper()

	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse account: %v", err)
	}
	return &account
}

func buildTransactionConfigRoot(t *testing.T, params map[uint32]*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for id, param := range params {
		value := cell.BeginCell().MustStoreRef(param).EndCell()
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(id)), value); err != nil {
			t.Fatalf("failed to store config param %d: %v", id, err)
		}
	}
	return dict.AsCell()
}

func buildTransactionMsgForwardPricesCell(t *testing.T, lumpPrice uint64, firstFrac uint16) *cell.Cell {
	t.Helper()

	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: lumpPrice,
		FirstFrac: firstFrac,
	})
	if err != nil {
		t.Fatalf("failed to build msg forward prices config: %v", err)
	}
	return priceCell
}

func buildTransactionActionList(t *testing.T, actions ...any) *cell.Cell {
	t.Helper()

	root := cell.BeginCell().EndCell()
	for _, action := range actions {
		next, err := tlb.ToCell(tlb.OutList{
			Prev: root,
			Out:  action,
		})
		if err != nil {
			t.Fatalf("failed to build action list: %v", err)
		}
		root = next
	}
	return root
}

func buildTransactionSizeLimitsCell(t *testing.T, maxMsgBits, maxMsgCells, maxLibraryCells, maxAccCells, maxMCAccCells uint32) *cell.Cell {
	t.Helper()

	return buildTransactionSizeLimitsCellWithPublicLibraries(t, maxMsgBits, maxMsgCells, maxLibraryCells, maxAccCells, maxMCAccCells, 256)
}

func buildTransactionSizeLimitsCellWithPublicLibraries(t *testing.T, maxMsgBits, maxMsgCells, maxLibraryCells, maxAccCells, maxMCAccCells, maxAccPublicLibraries uint32) *cell.Cell {
	t.Helper()

	limitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV2{
		MaxMsgBits:                  maxMsgBits,
		MaxMsgCells:                 maxMsgCells,
		MaxLibraryCells:             maxLibraryCells,
		MaxVMDataDepth:              512,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              512,
		MaxAccStateCells:            maxAccCells,
		MaxMCAccStateCells:          maxMCAccCells,
		MaxAccPublicLibraries:       maxAccPublicLibraries,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: 26,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}
	return limitsCell
}

func buildTransactionOutboundInternalCell(t *testing.T, amount uint64) *cell.Cell {
	t.Helper()

	return buildTransactionOutboundInternalCellWithBody(t, amount, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
}

func buildTransactionOutboundInternalCellWithBody(t *testing.T, amount uint64, body *cell.Cell) *cell.Cell {
	t.Helper()

	return buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), tonopsTestAddr, amount, body)
}

func buildTransactionOutboundInternalCellWithAddresses(t *testing.T, src, dst *address.Address, amount uint64, body *cell.Cell) *cell.Cell {
	t.Helper()

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     src,
		DstAddr:     dst,
		Amount:      tlb.FromNanoTONU(amount),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build outbound internal message: %v", err)
	}
	return msgCell
}

func buildTransactionOutboundInternalCellWithExtra(t *testing.T, amount uint64, extra *cell.Dictionary) *cell.Cell {
	t.Helper()

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled:     true,
		SrcAddr:         address.NewAddressNone(),
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(amount),
		ExtraCurrencies: extra,
		Body:            cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build outbound internal message with extra currencies: %v", err)
	}
	return msgCell
}

func buildTransactionTestNoneShardAccount(t *testing.T) *tlb.ShardAccount {
	t.Helper()

	return &tlb.ShardAccount{
		Account:       cell.BeginCell().MustStoreBoolBit(false).EndCell(),
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func TestTransactionMasterchainStateCellLimitStartsAtV12(t *testing.T) {
	masterchainAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x44}, 32))
	newCode := transactionTestCellChain(3)

	for _, tc := range []struct {
		name       string
		version    uint32
		wantExceed bool
	}{
		{name: "v11", version: 11},
		{name: "v12", version: 12, wantExceed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, tc.version),
				tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 3, 2),
			})
			acc := &transactionRuntimeAccount{
				addr:   masterchainAddr,
				status: tlb.AccountStatusActive,
				code:   cell.BeginCell().EndCell(),
			}

			exceeds, err := transactionAccountStateExceedsLimits(acc, newCode, nil, nil, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if exceeds != tc.wantExceed {
				t.Fatalf("exceeds limits = %t, want %t", exceeds, tc.wantExceed)
			}
		})
	}
}

func TestTransactionAccountAddressAnycastSerializationDisabledFromV10(t *testing.T) {
	anycastAddr := tonopsTestAddr.WithAnycast(address.NewAnycast(1, []byte{0x80}))
	rawData := append([]byte(nil), tonopsTestAddr.Data()...)
	rewrittenData := append([]byte(nil), tonopsTestAddr.Data()...)
	rewrittenData[0] |= 0x80
	v9Depth := uint64(1)
	for _, tc := range []struct {
		name        string
		version     uint32
		wantAnycast bool
		wantData    []byte
		wantDepth   *uint64
	}{
		{name: "v9", version: 9, wantAnycast: true, wantData: rawData, wantDepth: &v9Depth},
		{name: "v10", version: 10, wantData: rewrittenData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:    anycastAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(1000),
			}
			accountCell, accountState, _, err := buildTransactionAccountCell(acc, tlb.AccountStatusActive, big.NewInt(1000), nil, 1, uint32(tonopsTestTime.Unix()), nil, cell.BeginCell().EndCell(), nil, nil, nil, transactionTestConfigWithGlobalVersion(t, tc.version), nil)
			if err != nil {
				t.Fatal(err)
			}
			if (accountState.Address.Anycast() != nil) != tc.wantAnycast {
				t.Fatalf("result account anycast = %v, want present=%t", accountState.Address.Anycast(), tc.wantAnycast)
			}
			if !bytes.Equal(accountState.Address.Data(), tc.wantData) {
				t.Fatalf("result account data = %x, want %x", accountState.Address.Data(), tc.wantData)
			}
			if !transactionUint64PtrEqual(accountState.StateInit.Depth, tc.wantDepth) {
				t.Fatalf("result state depth = %v, want %v", accountState.StateInit.Depth, tc.wantDepth)
			}

			var parsed tlb.AccountState
			if err = tlb.Parse(&parsed, accountCell); err != nil {
				t.Fatalf("failed to parse account cell: %v", err)
			}
			if (parsed.Address.Anycast() != nil) != tc.wantAnycast {
				t.Fatalf("serialized account anycast = %v, want present=%t", parsed.Address.Anycast(), tc.wantAnycast)
			}
			if !bytes.Equal(parsed.Address.Data(), tc.wantData) {
				t.Fatalf("serialized account data = %x, want %x", parsed.Address.Data(), tc.wantData)
			}
			if !transactionUint64PtrEqual(parsed.StateInit.Depth, tc.wantDepth) {
				t.Fatalf("serialized state depth = %v, want %v", parsed.StateInit.Depth, tc.wantDepth)
			}
		})
	}
}

func transactionUint64PtrEqual(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestTransactionOutboundAnycastDestinationRewriteBeforeV10(t *testing.T) {
	wrongPrefix := []byte{tonopsTestAddr.Data()[0] ^ 0x80}
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(1, wrongPrefix))
	msgCell := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), dst, 100, cell.BeginCell().EndCell())

	normalized, err := transactionNormalizeOutboundMessage(msgCell, tonopsTestAddr, 10, uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	var msg tlb.Message
	if err = transactionParseCell(&msg, normalized); err != nil {
		t.Fatal(err)
	}
	gotAnycast := msg.AsInternal().DstAddr.Anycast()
	if gotAnycast == nil {
		t.Fatal("v9 normalized destination should keep anycast")
	}
	if transactionBit(gotAnycast.Prefix(), 0) != transactionBit(tonopsTestAddr.Data(), 0) {
		t.Fatalf("anycast prefix was not rewritten to account prefix")
	}

	_, err = transactionNormalizeOutboundMessage(msgCell, tonopsTestAddr, 10, uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, 10))
	if !errors.Is(err, errTransactionInvalidDestination) {
		t.Fatalf("v10 normalize error = %v, want invalid destination", err)
	}
}

func TestTransactionStateInitFixedPrefixLimitStartsAtV10(t *testing.T) {
	depth := uint64(9)
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  cell.BeginCell().EndCell(),
		Data:  cell.BeginCell().EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}
	addrData := append([]byte(nil), stateCell.Hash()...)
	addrData[0] ^= 0x80
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			DstAddr:   address.NewAddress(0, 0, addrData),
			StateInit: stateInit,
		},
	}

	for _, tc := range []struct {
		name     string
		version  uint32
		wantSkip bool
	}{
		{name: "v9", version: 9},
		{name: "v10", version: 10, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:   msg.AsInternal().DstAddr,
				status: tlb.AccountStatusUninit,
			}
			next, usedState, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, msg, false, transactionTestConfigWithGlobalVersion(t, tc.version))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantSkip {
				if skip == nil || skip.Type != tlb.ComputeSkipReasonBadState || usedState {
					t.Fatalf("skip=%+v usedState=%t, want bad_state without activation", skip, usedState)
				}
				return
			}
			if skip != nil || !usedState {
				t.Fatalf("skip=%+v usedState=%t, want activation", skip, usedState)
			}
			if next.stateDepth != nil {
				t.Fatalf("activated account state depth = %v, want nil", next.stateDepth)
			}
		})
	}
}

func TestTransactionStateInitFixedPrefixPreservedFromV10(t *testing.T) {
	depth := uint64(7)
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  cell.BeginCell().EndCell(),
		Data:  cell.BeginCell().EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			DstAddr:   address.NewAddress(0, 0, stateCell.Hash()),
			StateInit: stateInit,
		},
	}

	for _, tc := range []struct {
		name      string
		version   uint32
		wantDepth *uint64
	}{
		{name: "v9", version: 9},
		{name: "v10", version: 10, wantDepth: &depth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:   msg.AsInternal().DstAddr,
				status: tlb.AccountStatusUninit,
			}
			next, usedState, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, msg, false, transactionTestConfigWithGlobalVersion(t, tc.version))
			if err != nil {
				t.Fatal(err)
			}
			if skip != nil || !usedState {
				t.Fatalf("skip=%+v usedState=%t, want activation", skip, usedState)
			}
			if tc.wantDepth == nil {
				if next.stateDepth != nil {
					t.Fatalf("activated account state depth = %v, want nil", next.stateDepth)
				}
				return
			}
			if next.stateDepth == nil || *next.stateDepth != *tc.wantDepth {
				t.Fatalf("activated account state depth = %v, want %d", next.stateDepth, *tc.wantDepth)
			}
		})
	}

	t.Run("v10_zero_depth_normalized", func(t *testing.T) {
		zeroDepth := uint64(0)
		zeroStateInit := &tlb.StateInit{
			Depth: &zeroDepth,
			Code:  cell.BeginCell().EndCell(),
			Data:  cell.BeginCell().EndCell(),
		}
		zeroStateCell, err := tlb.ToCell(zeroStateInit)
		if err != nil {
			t.Fatalf("failed to build state init: %v", err)
		}
		zeroMsg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				DstAddr:   address.NewAddress(0, 0, zeroStateCell.Hash()),
				StateInit: zeroStateInit,
			},
		}
		acc := &transactionRuntimeAccount{
			addr:   zeroMsg.AsInternal().DstAddr,
			status: tlb.AccountStatusUninit,
		}
		next, usedState, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusUninit, false, zeroMsg, false, transactionTestConfigWithGlobalVersion(t, 10))
		if err != nil {
			t.Fatal(err)
		}
		if skip != nil || !usedState {
			t.Fatalf("skip=%+v usedState=%t, want activation", skip, usedState)
		}
		if next.stateDepth != nil {
			t.Fatalf("activated account state depth = %v, want nil", next.stateDepth)
		}
	})
}

func buildTransactionGasLimitsCell(t *testing.T, freezeDue, deleteDue uint64) *cell.Cell {
	t.Helper()

	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasPrice:       1,
		GasLimit:       1_000_000,
		GasCredit:      10_000,
		BlockGasLimit:  1_000_000,
		FreezeDueLimit: freezeDue,
		DeleteDueLimit: deleteDue,
	})
	if err != nil {
		t.Fatalf("failed to build gas limits config: %v", err)
	}
	return gasCell
}

func TestTransactionSpecialGasFullStartsAtV5(t *testing.T) {
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		SpecialGasLimit:         1_000,
		FlatGasLimit:            10,
		FlatGasPrice:            100,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices: %v", err)
	}

	for _, tc := range []struct {
		name      string
		version   uint32
		wantLimit int64
	}{
		{name: "v4", version: 4, wantLimit: 110},
		{name: "v5", version: 5, wantLimit: 1_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion:      transactionTestGlobalVersionCell(t, tc.version),
				tlb.ConfigParamGasPricesBasechain: gasCell,
			})
			gas := transactionMessageGas(TransactionEmulationConfig{}, cfg, tonopsTestAddr, big.NewInt(10_000), big.NewInt(200), tlb.MsgTypeInternal, true)
			if gas.Max != 1_000 || gas.Limit != tc.wantLimit || gas.Remaining != tc.wantLimit {
				t.Fatalf("gas = %+v, want max=1000 limit=%d", gas, tc.wantLimit)
			}
		})
	}
}

func TestEmulateTransactionExternalCommit(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if !res.Accepted {
		t.Fatal("expected external transaction to be accepted")
	}
	if res.Transaction == nil || res.TransactionCell == nil {
		t.Fatal("expected serialized transaction result")
	}
	if res.AccountState == nil || res.AccountCell == nil || res.ShardAccount == nil || res.ShardAccountCell == nil {
		t.Fatal("expected committed account state")
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), newData.Hash()) {
		t.Fatalf("unexpected committed data:\nwant=%s\ngot=%s", newData.Dump(), res.AccountState.StateInit.Data.Dump())
	}
	if res.Transaction.LT != uint64(transactionTestLogicalTime) {
		t.Fatalf("unexpected transaction lt: %d", res.Transaction.LT)
	}
	if res.AccountState.LastTransactionLT != res.Transaction.LT+1 {
		t.Fatalf("unexpected committed account lt: got=%d want=%d", res.AccountState.LastTransactionLT, res.Transaction.LT+1)
	}
	if res.ShardAccount.LastTransLT != res.Transaction.LT {
		t.Fatalf("unexpected shard lt: got=%d want=%d", res.ShardAccount.LastTransLT, res.Transaction.LT)
	}
	if !bytes.Equal(res.ShardAccount.LastTransHash, res.TransactionCell.Hash()) {
		t.Fatal("shard last tx hash should match transaction hash")
	}
	if !bytes.Equal(res.Transaction.StateUpdate.OldHash, shard.Account.Hash()) {
		t.Fatal("state update old hash should match original account hash")
	}
	if !bytes.Equal(res.Transaction.StateUpdate.NewHash, res.AccountCell.Hash()) {
		t.Fatal("state update new hash should match committed account hash")
	}
	if res.Transaction.TotalFees.Coins.Nano().Sign() != 0 {
		t.Fatalf("expected zero total fees without config pricing, got %s", res.Transaction.TotalFees.Coins.String())
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if !desc.CreditFirst {
		t.Fatal("external transaction should have credit_first=true")
	}
	if desc.ActionPhase == nil {
		t.Fatal("successful compute should materialize action phase even without actions")
	}
	vmPhase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		t.Fatalf("unexpected compute phase type %T", desc.ComputePhase.Phase)
	}
	if !vmPhase.Success || vmPhase.Details.ExitCode != 0 {
		t.Fatalf("unexpected compute phase result: success=%t exit=%d", vmPhase.Success, vmPhase.Details.ExitCode)
	}
	if vmPhase.Details.VMSteps != res.Steps {
		t.Fatalf("unexpected vm step count: got=%d want=%d", vmPhase.Details.VMSteps, res.Steps)
	}
}

func TestEmulateTransactionCommittedThrowRunsActionPhase(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 10)
	code := makeTransactionExternalCommitThenThrowCode(t, newData, outMsg, 304)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if !res.Accepted || !res.Committed {
		t.Fatalf("expected accepted committed compute, accepted=%t committed=%t", res.Accepted, res.Committed)
	}
	if res.ExitCode != 304 {
		t.Fatalf("exit code = %d, want 304", res.ExitCode)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected transaction description %T", res.Transaction.Description)
	}
	compute, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		t.Fatalf("unexpected compute phase %T", desc.ComputePhase.Phase)
	}
	if !compute.Success || desc.ActionPhase == nil || !desc.ActionPhase.Success || desc.Aborted {
		t.Fatalf("committed throw should run successful action phase: compute=%+v action=%+v aborted=%t", compute, desc.ActionPhase, desc.Aborted)
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), newData.Hash()) {
		t.Fatalf("unexpected committed data:\nwant=%s\ngot=%s", newData.Dump(), res.AccountState.StateInit.Data.Dump())
	}
	if res.Transaction.OutMsgCount != 1 {
		t.Fatalf("out msg count = %d, want 1", res.Transaction.OutMsgCount)
	}
}

func TestEmulateTransactionExternalStopOnAccept(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalAcceptThenFailCode(t)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	machine := NewTVM()
	res, err := machine.EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:      tonopsTestAddr,
		Now:          uint32(tonopsTestTime.Unix()),
		BlockLT:      transactionTestLogicalTime,
		LogicalTime:  transactionTestLogicalTime,
		StopOnAccept: true,
		ConfigRoot:   transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if !res.Accepted {
		t.Fatal("expected external transaction to be accepted")
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if res.Transaction == nil || res.TransactionCell == nil {
		t.Fatal("expected transaction after accept")
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), origData.Hash()) {
		t.Fatal("stop on accept should leave original data committed")
	}
}

func TestCheckExternalMessageAcceptedMatchesTransactionAccept(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalAcceptThenFailCode(t)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	var account tlb.AccountState
	if err = tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse account: %v", err)
	}

	txCfg := TransactionEmulationConfig{
		Address:      tonopsTestAddr,
		Now:          uint32(tonopsTestTime.Unix()),
		BlockLT:      transactionTestLogicalTime,
		LogicalTime:  transactionTestLogicalTime,
		StopOnAccept: true,
		ConfigRoot:   transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
	}
	checkCfg := CheckExternalMessageAcceptedConfig{
		Now:         txCfg.Now,
		BlockLT:     txCfg.BlockLT,
		LogicalTime: txCfg.LogicalTime,
		ConfigRoot:  txCfg.ConfigRoot,
	}

	full, err := NewTVM().EmulateTransaction(shard, msgCell, txCfg)
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	accepted, err := NewTVM().CheckExternalMessageAccepted(shard, &account, msgCell, msg, checkCfg)
	if err != nil {
		t.Fatalf("check external message accepted failed: %v", err)
	}
	if accepted != full.Accepted {
		t.Fatalf("accepted = %t, want %t", accepted, full.Accepted)
	}
}

func TestCheckExternalMessageAcceptedRejectsWithoutAccept(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	var account tlb.AccountState
	if err = tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse account: %v", err)
	}

	accepted, err := NewTVM().CheckExternalMessageAccepted(shard, &account, msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
	})
	if err != nil {
		t.Fatalf("check external message accepted failed: %v", err)
	}
	if accepted {
		t.Fatal("expected message without accept to be rejected")
	}
}

func TestCheckExternalMessageAcceptedChksigAlwaysSucceedPerRun(t *testing.T) {
	signature := make([]byte, 64)
	signature[0] = 1
	signature[63] = 2
	acceptBody := codeFromBuilders(t, funcsop.ACCEPT().Serialize())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()

	machine := NewTVM()
	for _, sigCase := range executionConfigSignatureCases {
		t.Run(sigCase.name, func(t *testing.T) {
			code := makeCheckExternalAcceptedChksigAlwaysCode(t, sigCase, signature, acceptBody)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

			msg := &tlb.ExternalMessage{
				DstAddr: tonopsTestAddr,
				Body:    body,
			}
			msgCell, err := tlb.ToCell(msg)
			if err != nil {
				t.Fatalf("failed to build external message: %v", err)
			}

			var account tlb.AccountState
			if err = tlb.Parse(&account, shard.Account); err != nil {
				t.Fatalf("failed to parse account: %v", err)
			}

			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					for _, tt := range []struct {
						name   string
						always bool
						want   bool
					}{
						{name: "default_rejects", always: false, want: false},
						{name: "configured_accepts", always: true, want: true},
						{name: "next_default_rejects", always: false, want: false},
					} {
						want := tt.want && version >= sigCase.minVersion
						t.Run(tt.name, func(t *testing.T) {
							accepted, err := machine.CheckExternalMessageAccepted(shard, &account, msgCell, msg, CheckExternalMessageAcceptedConfig{
								Now:                 uint32(tonopsTestTime.Unix()),
								BlockLT:             transactionTestLogicalTime,
								LogicalTime:         transactionTestLogicalTime,
								ConfigRoot:          transactionTestConfigWithGlobalVersion(t, uint32(version)).Root,
								ChksigAlwaysSucceed: tt.always,
							})
							if err != nil {
								t.Fatalf("check external message accepted failed: %v", err)
							}
							if accepted != want {
								t.Fatalf("accepted = %t, want %t", accepted, want)
							}
						})
					}
				})
			}
		})
	}
}

func makeCheckExternalAcceptedChksigAlwaysCode(t *testing.T, tt executionConfigSignatureCase, signature []byte, acceptBody *cell.Cell) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
	}
	if tt.fromSlice {
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(0)).Serialize())
	}

	builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()).Serialize())
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(2)).Serialize())
	}

	builders = append(builders, chksigAlwaysVariantOpcode(t, tt), execop.IFJMPREF(acceptBody).Serialize())
	return codeFromBuilders(t, builders...)
}

func TestCheckExternalMessageAcceptedRejectsInvalidStateInitLibraries(t *testing.T) {
	code := makeTransactionExternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	invalidLibs := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(cell.BeginCell().EndCell().Hash(), 256).EndCell()
	if err := invalidLibs.Set(key, cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("store invalid library: %v", err)
	}

	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		StateInit: &tlb.StateInit{
			Lib: invalidLibs,
		},
		Body: cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	accepted, err := NewTVM().CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
	})
	if err == nil {
		t.Fatal("invalid StateInit library should fail")
	}
	if accepted {
		t.Fatal("invalid StateInit library should not be accepted")
	}
}

func TestCheckExternalMessageAcceptedRejectsInvalidDestinationFromGlobalV10(t *testing.T) {
	code := makeTransactionExternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))
	msg := &tlb.ExternalMessage{
		DstAddr: address.NewAddressNone(),
		Body:    cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 10})
	if err != nil {
		t.Fatalf("failed to build global version cell: %v", err)
	}
	accepted, err := NewTVM().CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: versionCell,
		}),
	})
	if err == nil {
		t.Fatal("invalid inbound external destination should fail from global v10")
	}
	if accepted {
		t.Fatal("invalid inbound external destination should not be accepted")
	}
}

func TestCheckExternalMessageAcceptedNoGasSkipAndAutoNow(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	noGasShard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 0, 0)
	accepted, err := NewTVM().CheckExternalMessageAccepted(noGasShard, mustParseTransactionTestAccount(t, noGasShard), msgCell, msg, CheckExternalMessageAcceptedConfig{
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
	})
	if err != nil {
		t.Fatalf("no-gas check external message accepted failed: %v", err)
	}
	if accepted {
		t.Fatal("no-gas skip should not be accepted")
	}

	fundedShard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, 0)
	accepted, err = NewTVM().CheckExternalMessageAccepted(fundedShard, mustParseTransactionTestAccount(t, fundedShard), msgCell, msg, CheckExternalMessageAcceptedConfig{
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
	})
	if err != nil {
		t.Fatalf("auto-now check external message accepted failed: %v", err)
	}
	if !accepted {
		t.Fatal("funded external message should be accepted when Now is auto-filled")
	}
}

func TestEmulateTransactionInternalToNonExistCreatesUninitNoState(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	shard := buildTransactionTestNoneShardAccount(t)

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(123),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if res.Transaction == nil || res.AccountState == nil {
		t.Fatal("expected internal message to non-existing account to create a transaction")
	}
	if res.Transaction.OrigStatus != tlb.AccountStatusNonExist {
		t.Fatalf("unexpected original status: %s", res.Transaction.OrigStatus)
	}
	if res.Transaction.EndStatus != tlb.AccountStatusUninit {
		t.Fatalf("unexpected end status: %s", res.Transaction.EndStatus)
	}
	if res.AccountState.Status != tlb.AccountStatusUninit {
		t.Fatalf("unexpected committed account status: %s", res.AccountState.Status)
	}
	if got := res.AccountState.Balance.Nano().Uint64(); got != 123 {
		t.Fatalf("unexpected committed balance: got=%d want=123", got)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
	if !ok {
		t.Fatalf("unexpected compute phase type %T", desc.ComputePhase.Phase)
	}
	if skipped.Reason.Type != tlb.ComputeSkipReasonNoState {
		t.Fatalf("unexpected skip reason: %s", skipped.Reason.Type)
	}
	if !desc.Aborted || desc.ActionPhase != nil {
		t.Fatal("skipped compute should abort without action phase")
	}
}

func TestEmulateTransactionInternalToSuspendedUninitSkipsSuspended(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	stateInit := &tlb.StateInit{
		Code: makeTransactionInternalSuccessCode(t, cell.BeginCell().EndCell()),
		Data: cell.BeginCell().EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())
	shard := buildTransactionTestUninitShardAccount(t, addr, 0, tlb.StorageInfo{
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})

	suspended := cell.NewDict(288)
	suspendedKey := cell.BeginCell().
		MustStoreInt(int64(addr.Workchain()), 32).
		MustStoreSlice(addr.Data(), 256).
		EndCell()
	if err = suspended.Set(suspendedKey, cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("failed to store suspended address: %v", err)
	}
	suspendedCell, err := tlb.ToCell(&tlb.SuspendedAddressList{
		Addresses:      suspended,
		SuspendedUntil: now + 60,
	})
	if err != nil {
		t.Fatalf("failed to build suspended address list: %v", err)
	}

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000),
		StateInit:   stateInit,
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamSuspendedAddressList: suspendedCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
	if !ok {
		t.Fatalf("unexpected compute phase type %T", desc.ComputePhase.Phase)
	}
	if skipped.Reason.Type != tlb.ComputeSkipReasonSuspended {
		t.Fatalf("unexpected skip reason: %s", skipped.Reason.Type)
	}
	if res.AccountState.Status != tlb.AccountStatusUninit {
		t.Fatalf("unexpected committed account status: %s", res.AccountState.Status)
	}
}

func TestEmulateTransactionStorageDebtFreezesActiveAndRecordsDue(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)

	duePayment := tlb.FromNanoTONU(1000)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, 50, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     uint32(tonopsTestTime.Unix()) - 60,
		DuePayment:   &duePayment,
	})

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:        transactionTestGlobalVersionCell(t, 4),
			tlb.ConfigParamGasPricesBasechain:   buildTransactionGasLimitsCell(t, 100, 10_000),
			tlb.ConfigParamGasPricesMasterchain: buildTransactionGasLimitsCell(t, 100, 10_000),
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if res.Transaction.EndStatus != tlb.AccountStatusFrozen {
		t.Fatalf("unexpected end status: %s", res.Transaction.EndStatus)
	}
	if res.AccountState.Status != tlb.AccountStatusFrozen {
		t.Fatalf("unexpected committed account status: %s", res.AccountState.Status)
	}
	if got := res.Transaction.TotalFees.Coins.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected total fees: got=%d want=50", got)
	}
	if got := res.AccountState.Balance.Nano().Uint64(); got != 0 {
		t.Fatalf("unexpected committed balance: got=%d want=0", got)
	}
	if res.AccountState.StorageInfo.DuePayment == nil || res.AccountState.StorageInfo.DuePayment.Nano().Uint64() != 950 {
		t.Fatalf("unexpected due payment: %v", res.AccountState.StorageInfo.DuePayment)
	}

	stateInitCell, err := tlb.ToCell(&tlb.StateInit{
		Code: code,
		Data: origData,
	})
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}
	if !bytes.Equal(res.AccountState.StateHash, stateInitCell.Hash()) {
		t.Fatal("frozen state hash should match original active state")
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if desc.StoragePhase.StatusChange.Type != tlb.AccStatusChangeFrozen {
		t.Fatalf("unexpected storage status change: %s", desc.StoragePhase.StatusChange.Type)
	}
	if got := desc.StoragePhase.StorageFeesCollected.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected collected storage fee: got=%d want=50", got)
	}
	if desc.StoragePhase.StorageFeesDue == nil || desc.StoragePhase.StorageFeesDue.Nano().Uint64() != 950 {
		t.Fatalf("unexpected storage fees due: %v", desc.StoragePhase.StorageFeesDue)
	}
	skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
	if !ok {
		t.Fatalf("unexpected compute phase type %T", desc.ComputePhase.Phase)
	}
	if skipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("unexpected skip reason: %s", skipped.Reason.Type)
	}
}

func TestEmulateTransactionInternalCreditsBalance(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(internalMessageTestAmount),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if !res.Accepted {
		t.Fatal("expected internal transaction to be accepted")
	}

	wantBalance := new(big.Int).SetUint64(walletSendTestBalance + internalMessageTestAmount)
	if got := res.AccountState.Balance.Nano(); got.Cmp(wantBalance) != 0 {
		t.Fatalf("unexpected committed balance: got=%s want=%s", got.String(), wantBalance.String())
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), newData.Hash()) {
		t.Fatalf("unexpected committed data:\nwant=%s\ngot=%s", newData.Dump(), res.AccountState.StateInit.Data.Dump())
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if !desc.CreditFirst {
		t.Fatal("bounce-disabled internal message should credit before compute")
	}
	if desc.CreditPhase == nil {
		t.Fatal("expected credit phase for internal transaction")
	}
	if got := desc.CreditPhase.Credit.Coins.Nano().Uint64(); got != internalMessageTestAmount {
		t.Fatalf("unexpected credited amount: %d", got)
	}
	if res.Transaction.TotalFees.Coins.Nano().Sign() != 0 {
		t.Fatalf("expected zero total fees without pricing config, got %s", res.Transaction.TotalFees.Coins.String())
	}
}

func TestEmulateTransactionCollectsDuePaymentAndClearsIt(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 7})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}

	duePayment := tlb.FromNanoTONU(77)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     uint32(tonopsTestTime.Unix()) - 60,
		DuePayment:   &duePayment,
	})

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: versionCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	if got := res.Transaction.TotalFees.Coins.Nano().Uint64(); got != 77 {
		t.Fatalf("unexpected total fees: got=%d want=77", got)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if got := desc.StoragePhase.StorageFeesCollected.Nano().Uint64(); got != 77 {
		t.Fatalf("unexpected storage fees collected: got=%d want=77", got)
	}
	if res.AccountState.StorageInfo.DuePayment != nil {
		t.Fatal("expected due_payment to be cleared after collection")
	}
	if got := res.AccountState.StorageInfo.LastPaid; got != uint32(tonopsTestTime.Unix()) {
		t.Fatalf("unexpected last_paid: got=%d want=%d", got, uint32(tonopsTestTime.Unix()))
	}

	wantBalance := new(big.Int).SetUint64(walletSendTestBalance - 77)
	if got := res.AccountState.Balance.Nano(); got.Cmp(wantBalance) != 0 {
		t.Fatalf("unexpected committed balance: got=%s want=%s", got.String(), wantBalance.String())
	}
}

func TestTransactionInboundIHRFeeCreditStopsAtV12(t *testing.T) {
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(23),
		},
	}

	for _, tc := range []struct {
		name    string
		version uint32
		want    int64
	}{
		{name: "v11 credits legacy ihr fee", version: 11, want: 123},
		{name: "v12 treats field as extra flags", version: 12, want: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := transactionPrepareInitialPhases(&transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(1000),
				storageInfo: tlb.StorageInfo{
					StorageExtra: tlb.StorageExtraNone{},
				},
			}, msg, big.NewInt(0), big.NewInt(0), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, tc.version), transactionStorageDueLimits{})
			if err != nil {
				t.Fatal(err)
			}
			if got := prepared.msgBalance.grams.Int64(); got != tc.want {
				t.Fatalf("message balance = %d, want %d", got, tc.want)
			}
			if got := prepared.creditPhase.Credit.Coins.Nano().Int64(); got != tc.want {
				t.Fatalf("credit phase = %d, want %d", got, tc.want)
			}
			if got := prepared.balance.Int64(); got != 1000+tc.want {
				t.Fatalf("prepared balance = %d, want %d", got, 1000+tc.want)
			}
		})
	}
}

func TestTransactionStorageDuePaymentClearStartsAtV7(t *testing.T) {
	duePayment := tlb.FromNanoTONU(77)
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
		},
	}

	for _, tc := range []struct {
		name          string
		version       uint32
		wantDueRemain bool
	}{
		{name: "v6", version: 6, wantDueRemain: true},
		{name: "v7", version: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithGlobalVersion(t, tc.version)
			acc := &transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(1000),
				storageInfo: tlb.StorageInfo{
					StorageExtra: tlb.StorageExtraNone{},
					DuePayment:   &duePayment,
				},
			}

			prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(77), big.NewInt(0), uint32(tonopsTestTime.Unix()), cfg, transactionStorageDueLimits{})
			if err != nil {
				t.Fatal(err)
			}

			if got := prepared.storagePhase.StorageFeesCollected.Nano().Uint64(); got != 77 {
				t.Fatalf("storage fees collected = %d, want 77", got)
			}
			if tc.wantDueRemain {
				if prepared.duePayment == nil || prepared.duePayment.Nano().Uint64() != 77 {
					t.Fatalf("due payment = %v, want 77", prepared.duePayment)
				}
			} else if prepared.duePayment != nil {
				t.Fatalf("due payment = %v, want nil", prepared.duePayment)
			}
		})
	}
}

func TestTransactionStorageDuePaymentPersistStartsAtV4(t *testing.T) {
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
		},
	}

	for _, tc := range []struct {
		name       string
		version    uint32
		wantDueNil bool
	}{
		{name: "v3", version: 3, wantDueNil: true},
		{name: "v4", version: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(50),
				storageInfo: tlb.StorageInfo{
					StorageExtra: tlb.StorageExtraNone{},
				},
			}

			prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(100), big.NewInt(0), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, tc.version), transactionStorageDueLimits{
				freezeDue: big.NewInt(1_000_000),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := prepared.storagePhase.StorageFeesDue.Nano().Uint64(); got != 50 {
				t.Fatalf("storage phase due = %d, want 50", got)
			}
			if tc.wantDueNil {
				if prepared.duePayment != nil {
					t.Fatalf("persisted due payment = %v, want nil", prepared.duePayment)
				}
				return
			}
			if prepared.duePayment == nil || prepared.duePayment.Nano().Uint64() != 50 {
				t.Fatalf("persisted due payment = %v, want 50", prepared.duePayment)
			}
		})
	}
}

func TestEmulateTransactionConfigGlobalVersionControlsOpcodeDispatch(t *testing.T) {
	code := codeFromBuilders(t, funcsop.GASCONSUMED().Serialize())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, uint32(tonopsTestTime.Unix()))
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, 3).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if res.Accepted {
		t.Fatal("external message should not be accepted after invalid opcode")
	}
	if res.ExitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("exit code = %d, want invalid opcode", res.ExitCode)
	}
}

func TestEmulateTransactionChksigAlwaysSucceedPerRun(t *testing.T) {
	data := cell.BeginCell().EndCell()
	body := cell.BeginCell().EndCell()
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	signature := make([]byte, 64)
	signature[0] = 0xA5
	signature[63] = 0x5A

	for _, tt := range executionConfigSignatureCases {
		t.Run(tt.name, func(t *testing.T) {
			code := makeMessageChksigAlwaysVariantCode(t, tt, signature)

			for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, uint32(tonopsTestTime.Unix()))
					cfg := TransactionEmulationConfig{
						Address:     tonopsTestAddr,
						Now:         uint32(tonopsTestTime.Unix()),
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
						Gas: vmcore.NewGas(vmcore.GasConfig{
							Max:    walletSendTestGasMax,
							Credit: walletSendTestCredit,
						}),
					}

					if tt.minVersion > 0 && version < uint32(tt.minVersion) {
						res := runTransactionChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, true)
						if res.exit != vmerr.CodeInvalidOpcode {
							t.Fatalf("%s v%d always=true exit=%d, want invalid opcode", tt.name, version, res.exit)
						}
						return
					}

					assertMessageChksigAlwaysVariant(t, tt, version, false, runTransactionChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, false), false)
					assertMessageChksigAlwaysVariant(t, tt, version, true, runTransactionChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, true), true)
					assertMessageChksigAlwaysVariant(t, tt, version, false, runTransactionChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, false), false)
				})
			}
		})
	}
}

func runTransactionChksigAlwaysVariant(t *testing.T, machine *TVM, shard *tlb.ShardAccount, msgCell *cell.Cell, cfg TransactionEmulationConfig, tt executionConfigSignatureCase, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	res, err := machine.EmulateTransaction(shard, msgCell, cfg)
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("EmulateTransaction %s always=%v failed: %v", tt.name, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop transaction %s result always=%v: %v", tt.name, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
}

func TestEmulateTransactionAdvancesLastPaidWithoutStoragePricing(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     uint32(tonopsTestTime.Unix()) - 60,
	})

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	if got := res.AccountState.StorageInfo.LastPaid; got != uint32(tonopsTestTime.Unix()) {
		t.Fatalf("unexpected last_paid: got=%d want=%d", got, uint32(tonopsTestTime.Unix()))
	}
}

func TestEmulateTransactionRejectsTimeBeforeLastPaid(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     uint32(tonopsTestTime.Unix()) + 1,
	})

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	if _, err = NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	}); err == nil {
		t.Fatal("expected emulation to fail when now is before last_paid")
	}
}

func TestEmulateTransactionActionSendMsgAccountsForwardFees(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	code := makeTransactionInternalSendCode(t, outMsg, newData, 0)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 10_000, uint32(tonopsTestTime.Unix()))
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)

	inMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build inbound message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, inMsg, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		}),
		Gas: vmcore.NewGas(vmcore.GasConfig{Max: 1_000_000, Limit: 1_000_000}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	if got := res.AccountState.Balance.Nano().Uint64(); got != 9000 {
		t.Fatalf("unexpected committed balance: got=%d want=9000", got)
	}
	if got := res.Transaction.TotalFees.Coins.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected total fees: got=%d want=50", got)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if desc.Aborted {
		t.Fatal("successful action phase should not abort")
	}
	if desc.ActionPhase == nil || !desc.ActionPhase.Success || desc.ActionPhase.NoFunds {
		t.Fatalf("unexpected action phase: %+v", desc.ActionPhase)
	}
	if desc.ActionPhase.TotalFwdFees == nil || desc.ActionPhase.TotalFwdFees.Nano().Uint64() != 100 {
		t.Fatalf("unexpected total fwd fees: %v", desc.ActionPhase.TotalFwdFees)
	}
	if desc.ActionPhase.TotalActionFees == nil || desc.ActionPhase.TotalActionFees.Nano().Uint64() != 50 {
		t.Fatalf("unexpected total action fees: %v", desc.ActionPhase.TotalActionFees)
	}
	if desc.ActionPhase.MessagesCreated != 1 {
		t.Fatalf("unexpected messages created: %d", desc.ActionPhase.MessagesCreated)
	}

	out, err := res.Transaction.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to read outbound messages: %v", err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("unexpected outbound messages: %+v", out)
	}
	gotMsg := out[0].AsInternal()
	if got := gotMsg.Amount.Nano().Uint64(); got != 900 {
		t.Fatalf("unexpected outbound amount: got=%d want=900", got)
	}
	if got := gotMsg.FwdFee.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected outbound fwd fee: got=%d want=50", got)
	}
	if gotMsg.CreatedLT != uint64(transactionTestLogicalTime)+1 {
		t.Fatalf("unexpected outbound created lt: got=%d", gotMsg.CreatedLT)
	}
	if gotMsg.CreatedAt != uint32(tonopsTestTime.Unix()) {
		t.Fatalf("unexpected outbound created at: got=%d", gotMsg.CreatedAt)
	}
	if gotMsg.SrcAddr.StringRaw() != tonopsTestAddr.StringRaw() {
		t.Fatalf("unexpected outbound source: got=%s want=%s", gotMsg.SrcAddr.StringRaw(), tonopsTestAddr.StringRaw())
	}
}

func TestEmulateTransactionActionSendMsgClearsOutboundBouncedFlag(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1000),
		Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build outbound internal message: %v", err)
	}
	code := makeTransactionInternalSendCode(t, outMsg, newData, 0)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 10_000, uint32(tonopsTestTime.Unix()))

	inMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build inbound message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, inMsg, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamMsgForwardPricesBasechain:   buildTransactionMsgForwardPricesCell(t, 100, 1<<15),
			tlb.ConfigParamMsgForwardPricesMasterchain: buildTransactionMsgForwardPricesCell(t, 100, 1<<15),
		}),
		Gas: vmcore.NewGas(vmcore.GasConfig{Max: 1_000_000, Limit: 1_000_000}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	out, err := res.Transaction.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to read outbound messages: %v", err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("unexpected outbound messages: %+v", out)
	}
	if out[0].AsInternal().Bounced {
		t.Fatal("outbound internal message must clear bounced flag")
	}
}

func TestEmulateTransactionActionSendMsgNoFundsAbortsWithoutCommit(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	code := makeTransactionInternalSendCode(t, outMsg, newData, 0)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 500, uint32(tonopsTestTime.Unix()))

	inMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build inbound message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, inMsg, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas:         vmcore.NewGas(vmcore.GasConfig{Max: 1_000_000, Limit: 1_000_000}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	if got := res.AccountState.Balance.Nano().Uint64(); got != 500 {
		t.Fatalf("unexpected committed balance: got=%d want=500", got)
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), origData.Hash()) {
		t.Fatal("action failure should not commit computed data")
	}
	if res.Transaction.OutMsgCount != 0 {
		t.Fatalf("unexpected outbound message count: %d", res.Transaction.OutMsgCount)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if !desc.Aborted {
		t.Fatal("no-funds action failure should abort transaction")
	}
	if desc.ActionPhase == nil {
		t.Fatal("expected action phase for no-funds send")
	}
	if desc.ActionPhase.Success || !desc.ActionPhase.Valid || !desc.ActionPhase.NoFunds || desc.ActionPhase.ResultCode != 37 {
		t.Fatalf("unexpected action phase: %+v", desc.ActionPhase)
	}
}

func TestTVM14ActionFailureRestoresConsumedMessageBalanceRemaining(t *testing.T) {
	msgBalance, err := transactionCurrencyFromParts(big.NewInt(500), nil)
	if err != nil {
		t.Fatal(err)
	}
	versionConfig := func(version uint32) transactionConfig {
		t.Helper()
		versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
		if err != nil {
			t.Fatalf("failed to build global version cell: %v", err)
		}
		return transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
			Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion: versionCell,
			}),
		})
	}
	apply := func(version uint32) *transactionActionApplyResult {
		t.Helper()
		actions := buildTransactionActionList(t,
			tlb.ActionSendMsg{Mode: 64, Msg: buildTransactionOutboundInternalCell(t, 1)},
			tlb.ActionSendMsg{Mode: 16, Msg: buildTransactionOutboundInternalCell(t, 1000)},
		)
		acc := &transactionRuntimeAccount{
			addr:    tonopsTestAddr,
			status:  tlb.AccountStatusActive,
			code:    cell.BeginCell().EndCell(),
			data:    cell.BeginCell().EndCell(),
			balance: big.NewInt(1000),
		}
		res, err := transactionApplyActions(acc, &MessageExecutionResult{
			Accepted: true,
			ExecutionResult: ExecutionResult{
				ExitCode:  0,
				Data:      cell.BeginCell().EndCell(),
				Actions:   actions,
				Committed: true,
			},
		}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), versionConfig(version), big.NewInt(1000), nil, msgBalance, big.NewInt(0))
		if err != nil {
			t.Fatalf("apply actions v%d failed: %v", version, err)
		}
		if res.phase == nil || res.phase.Success || res.phase.ResultCode != 37 || !res.bounce {
			t.Fatalf("unexpected action failure v%d: phase=%+v bounce=%t", version, res.phase, res.bounce)
		}
		return res
	}

	v13 := apply(13)
	if got := v13.msgBalanceRemaining.grams.Int64(); got != 0 {
		t.Fatalf("v13 message balance remaining = %d, want 0", got)
	}
	v14 := apply(14)
	if got := v14.msgBalanceRemaining.grams.Int64(); got != 500 {
		t.Fatalf("v14 message balance remaining = %d, want 500", got)
	}
}

func TestTransactionFrozenHashEqualsAddressEndStatusStartsAtV13(t *testing.T) {
	acc := &transactionRuntimeAccount{addr: tonopsTestAddr}
	for _, tc := range []struct {
		name              string
		version           uint32
		wantTxStatus      tlb.AccountStatus
		wantAccountStatus tlb.AccountStatus
	}{
		{name: "v12", version: 12, wantTxStatus: tlb.AccountStatusFrozen, wantAccountStatus: tlb.AccountStatusUninit},
		{name: "v13", version: 13, wantTxStatus: tlb.AccountStatusUninit, wantAccountStatus: tlb.AccountStatusUninit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateHash := append([]byte(nil), tonopsTestAddr.Data()...)
			txStatus, accountStatus, nextHash, err := transactionNormalizeFrozenFinalState(acc, tlb.AccountStatusFrozen, nil, nil, nil, stateHash, transactionTestConfigWithGlobalVersion(t, tc.version))
			if err != nil {
				t.Fatal(err)
			}
			if txStatus != tc.wantTxStatus {
				t.Fatalf("tx status = %s, want %s", txStatus, tc.wantTxStatus)
			}
			if accountStatus != tc.wantAccountStatus {
				t.Fatalf("account status = %s, want %s", accountStatus, tc.wantAccountStatus)
			}
			if nextHash != nil {
				t.Fatalf("state hash = %x, want nil", nextHash)
			}
		})
	}
}

func TestEmulateTransactionActionBounceRequiresMode16(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)

	for _, tc := range []struct {
		name       string
		mode       uint8
		wantBounce bool
	}{
		{name: "no-bounce-on-fail", mode: 0, wantBounce: false},
		{name: "bounce-on-fail", mode: 16, wantBounce: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := makeTransactionInternalSendCode(t, outMsg, newData, tc.mode)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 500, uint32(tonopsTestTime.Unix()))
			inMsg, err := tlb.ToCell(&tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(100),
				Body:        cell.BeginCell().EndCell(),
			})
			if err != nil {
				t.Fatalf("failed to build inbound message: %v", err)
			}

			res, err := NewTVM().EmulateTransaction(shard, inMsg, TransactionEmulationConfig{
				Address:     tonopsTestAddr,
				Now:         uint32(tonopsTestTime.Unix()),
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
				Gas:         vmcore.NewGas(vmcore.GasConfig{Max: 1_000_000, Limit: 1_000_000}),
			})
			if err != nil {
				t.Fatalf("emulate transaction failed: %v", err)
			}

			desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
			if !ok {
				t.Fatalf("unexpected description type %T", res.Transaction.Description)
			}
			if desc.BouncePhase != nil != tc.wantBounce {
				t.Fatalf("unexpected bounce phase: got=%v want=%t", desc.BouncePhase, tc.wantBounce)
			}
			if got := res.Transaction.OutMsgCount; got != map[bool]uint16{false: 0, true: 1}[tc.wantBounce] {
				t.Fatalf("unexpected outbound count: got=%d", got)
			}
		})
	}
}

func TestTransactionApplyActionsReserveCurrencyAffectsLaterSends(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 200)
	actions := buildTransactionActionList(t,
		tlb.ActionReserveCurrency{
			Mode: 0,
			Currency: tlb.CurrencyCollection{
				Coins: tlb.FromNanoTONU(700),
			},
		},
		tlb.ActionSendMsg{
			Mode: 0,
			Msg:  outMsg,
		},
	)
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		code:    cell.BeginCell().EndCell(),
		data:    origData,
		balance: big.NewInt(1000),
	}

	res, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      origData,
			Actions:   actions,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply actions failed: %v", err)
	}
	if res.phase == nil || !res.phase.Success {
		t.Fatalf("unexpected action phase: %+v", res.phase)
	}
	if got := res.balance.Uint64(); got != 800 {
		t.Fatalf("unexpected remaining balance: got=%d want=800", got)
	}
	if len(res.outMsgs) != 1 {
		t.Fatalf("unexpected output count: %d", len(res.outMsgs))
	}
}

func TestTransactionApplyActionsSendMode2SkipsInvalidExtraFlags(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		IHRFee:      tlb.FromNanoTONU(4),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build outbound message: %v", err)
	}
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{
		Mode: 2,
		Msg:  msgCell,
	})
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: big.NewInt(1000),
	}

	res, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   actions,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply actions failed: %v", err)
	}
	if res.phase == nil || !res.phase.Success || res.phase.SkippedActions != 1 || res.phase.MessagesCreated != 0 {
		t.Fatalf("unexpected skipped action phase: %+v", res.phase)
	}
	if got := res.balance.Uint64(); got != 1000 {
		t.Fatalf("skipped send should not debit balance, got=%d", got)
	}
}

func TestTransactionApplyActionsMalformedSendPrepassSkipAndBounce(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: big.NewInt(1000),
	}

	for _, tc := range []struct {
		name       string
		mode       uint8
		wantCode   int32
		wantSkip   uint16
		wantBounce bool
	}{
		{name: "skip malformed", mode: 2, wantCode: 0, wantSkip: 1},
		{name: "bounce malformed", mode: 16, wantCode: 34, wantBounce: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			malformed := cell.BeginCell().
				MustStoreRef(cell.BeginCell().EndCell()).
				MustStoreUInt(0x0ec3c86d, 32).
				MustStoreUInt(uint64(tc.mode), 8).
				EndCell()
			res, err := transactionApplyActions(acc, &MessageExecutionResult{
				Accepted: true,
				ExecutionResult: ExecutionResult{
					ExitCode:  0,
					Data:      data,
					Actions:   malformed,
					Committed: true,
				},
			}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
			if err != nil {
				t.Fatalf("apply actions failed: %v", err)
			}
			if res.phase == nil || res.phase.ResultCode != tc.wantCode || res.phase.SkippedActions != tc.wantSkip || res.bounce != tc.wantBounce {
				t.Fatalf("unexpected action phase: phase=%+v bounce=%t", res.phase, res.bounce)
			}
			if tc.wantCode == 0 && !res.phase.Success {
				t.Fatalf("skipped malformed action should leave successful phase: %+v", res.phase)
			}
		})
	}

	oversizedData := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	firstSkipped := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(2, 8).
		EndCell()
	skippedOnly := cell.BeginCell().
		MustStoreRef(firstSkipped).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(2, 8).
		EndCell()
	res, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      oversizedData,
			Actions:   skippedOnly,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, 13),
			tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
		}),
	}), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply skipped-only state-limit actions failed: %v", err)
	}
	if res.phase == nil || res.phase.ResultArg == nil || *res.phase.ResultArg != 1 {
		t.Fatalf("skipped-only state-limit result arg = %+v, want 1", res.phase)
	}
	if res.phase.ResultCode != 50 || res.phase.SkippedActions != 2 || res.phase.MessagesCreated != 0 {
		t.Fatalf("unexpected skipped-only state-limit phase: %+v", res.phase)
	}
}

func TestTransactionApplyActionsChangeLibraryAndStateLimit(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	lib := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: big.NewInt(1000),
	}

	actions := buildTransactionActionList(t, tlb.ActionChangeLibrary{
		Mode: 2,
		LibRef: tlb.LibRefRef{
			Library: lib,
		},
	})
	res, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   actions,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionConfig{}, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply library action failed: %v", err)
	}
	if res.phase == nil || !res.phase.Success || res.nextLibraries == nil {
		t.Fatalf("library action did not succeed: %+v", res.phase)
	}
	stored, err := res.nextLibraries.LoadValueByIntKey(new(big.Int).SetBytes(lib.Hash()))
	if err != nil {
		t.Fatalf("library was not stored: %v", err)
	}
	isPublic, err := stored.LoadBoolBit()
	if err != nil || !isPublic {
		t.Fatalf("library should be public, got %t / %v", isPublic, err)
	}

	failAfterLib := buildTransactionActionList(t,
		tlb.ActionChangeLibrary{
			Mode: 2,
			LibRef: tlb.LibRefRef{
				Library: lib,
			},
		},
		tlb.ActionSendMsg{
			Mode: 0,
			Msg:  buildTransactionOutboundInternalCell(t, 5000),
		},
	)
	failedRes, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   failAfterLib,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionConfig{}, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply failing action list failed: %v", err)
	}
	if failedRes.phase == nil || failedRes.phase.Success || failedRes.phase.ResultCode != 37 {
		t.Fatalf("expected send failure after library change, got %+v", failedRes.phase)
	}
	if _, err = failedRes.nextLibraries.LoadValueByIntKey(new(big.Int).SetBytes(lib.Hash())); err != nil {
		t.Fatalf("library change should survive later action failure: %v", err)
	}

	oversizedCode := cell.BeginCell().MustStoreUInt(0xDD, 8).MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	limitActions := buildTransactionActionList(t, tlb.ActionSetCode{NewCode: oversizedCode})
	limitRes, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   limitActions,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamSizeLimits: buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
		}),
	}), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply state-limit action failed: %v", err)
	}
	if limitRes.phase == nil || limitRes.phase.Success || limitRes.phase.ResultCode != 50 || !limitRes.bounce {
		t.Fatalf("expected state-limit action failure with bounce, got phase=%+v bounce=%t", limitRes.phase, limitRes.bounce)
	}

	sendThenLimit := buildTransactionActionList(t,
		tlb.ActionSendMsg{
			Mode: 1,
			Msg:  buildTransactionOutboundInternalCell(t, 100),
		},
		tlb.ActionSetCode{NewCode: oversizedCode},
	)
	sendThenLimitRes, err := transactionApplyActions(acc, &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   sendThenLimit,
			Committed: true,
		},
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamSizeLimits: buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
		}),
	}), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
	if err != nil {
		t.Fatalf("apply send then state-limit action failed: %v", err)
	}
	if sendThenLimitRes.phase == nil || sendThenLimitRes.phase.ResultArg == nil || *sendThenLimitRes.phase.ResultArg != 1 {
		t.Fatalf("state-limit result arg = %v, want 1", sendThenLimitRes.phase)
	}
	if sendThenLimitRes.phase.MessagesCreated != 1 || len(sendThenLimitRes.outMsgs) != 0 {
		t.Fatalf("state-limit phase should count but not commit message: phase=%+v out=%d", sendThenLimitRes.phase, len(sendThenLimitRes.outMsgs))
	}
}

func TestTransactionApplyActionsStateLimitRestoresMessageBalanceFromV14(t *testing.T) {
	data := cell.BeginCell().EndCell()
	oversizedCode := cell.BeginCell().
		MustStoreUInt(0xDD, 8).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	actions := buildTransactionActionList(t,
		tlb.ActionSendMsg{
			Mode: 65,
			Msg:  buildTransactionOutboundInternalCell(t, 100),
		},
		tlb.ActionSetCode{NewCode: oversizedCode},
	)
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusActive,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: big.NewInt(1_000_000),
	}

	for _, tc := range []struct {
		version int
		want    int64
	}{
		{version: 13, want: 0},
		{version: 14, want: 500},
	} {
		t.Run(fmt.Sprintf("v%d", tc.version), func(t *testing.T) {
			msgBalance, err := transactionCurrencyFromParts(big.NewInt(500), nil)
			if err != nil {
				t.Fatalf("failed to build message balance: %v", err)
			}
			res, err := transactionApplyActions(acc, &MessageExecutionResult{
				Accepted: true,
				ExecutionResult: ExecutionResult{
					ExitCode:  0,
					Data:      data,
					Actions:   actions,
					Committed: true,
				},
			}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, uint32(tc.version)),
				tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
			}), big.NewInt(1_000_000), nil, msgBalance, big.NewInt(0))
			if err != nil {
				t.Fatalf("apply actions failed: %v", err)
			}
			if res.phase == nil || res.phase.Success || res.phase.ResultCode != 50 || res.phase.MessagesCreated != 1 || len(res.outMsgs) != 0 {
				t.Fatalf("unexpected state-limit failure result: phase=%+v out=%d", res.phase, len(res.outMsgs))
			}
			if got := res.msgBalanceRemaining.grams.Int64(); got != tc.want {
				t.Fatalf("v%d message balance remaining = %d, want %d", tc.version, got, tc.want)
			}
		})
	}
}

func TestEmulateTransactionBounceableNoStateGeneratesBouncePhase(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	shard := buildTransactionTestNoneShardAccount(t)
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13, Capabilities: 4})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1000),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               versionCell,
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}
	if res.Transaction.EndStatus != tlb.AccountStatusNonExist {
		t.Fatalf("unexpected end status: %s", res.Transaction.EndStatus)
	}
	if got := res.Transaction.TotalFees.Coins.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected total fees: got=%d want=50", got)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if !desc.Aborted || desc.ActionPhase != nil || desc.BouncePhase == nil {
		t.Fatalf("unexpected transaction phases: aborted=%t action=%v bounce=%v", desc.Aborted, desc.ActionPhase, desc.BouncePhase)
	}
	bounceOK, ok := desc.BouncePhase.Phase.(tlb.BouncePhaseOk)
	if !ok {
		t.Fatalf("unexpected bounce phase type %T", desc.BouncePhase.Phase)
	}
	if got := bounceOK.MsgFees.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected bounce msg fees: got=%d want=50", got)
	}
	if got := bounceOK.FwdFees.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected bounce fwd fees: got=%d want=50", got)
	}

	out, err := res.Transaction.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to read outbound messages: %v", err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("unexpected outbound messages: %+v", out)
	}
	bounced := out[0].AsInternal()
	if !bounced.Bounced || bounced.Bounce {
		t.Fatalf("unexpected bounce flags: bounce=%t bounced=%t", bounced.Bounce, bounced.Bounced)
	}
	if bounced.SrcAddr.StringRaw() != tonopsTestAddr.StringRaw() {
		t.Fatalf("unexpected bounce source: got=%s want=%s", bounced.SrcAddr.StringRaw(), tonopsTestAddr.StringRaw())
	}
	if bounced.DstAddr.StringRaw() != internalEmulationSrcAddr.StringRaw() {
		t.Fatalf("unexpected bounce destination: got=%s want=%s", bounced.DstAddr.StringRaw(), internalEmulationSrcAddr.StringRaw())
	}
	if got := bounced.Amount.Nano().Uint64(); got != 900 {
		t.Fatalf("unexpected bounce amount: got=%d want=900", got)
	}
	if got := bounced.FwdFee.Nano().Uint64(); got != 50 {
		t.Fatalf("unexpected bounce fwd fee: got=%d want=50", got)
	}
}

func TestEmulateTransactionBouncedInboundCanBounceAgain(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13, Capabilities: 4})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1000),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               versionCell,
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	if desc.BouncePhase == nil {
		t.Fatal("expected bounced inbound message with bounce flag to produce bounce phase")
	}
	if res.Transaction.OutMsgCount != 1 {
		t.Fatalf("out message count = %d, want 1", res.Transaction.OutMsgCount)
	}
}

func TestEmulateTransactionNewBounceBodyFormat(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	shard := buildTransactionTestNoneShardAccount(t)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: 100,
		CellPrice: 1 << 16,
		FirstFrac: 1 << 15,
	})
	if err != nil {
		t.Fatalf("failed to build msg forward prices config: %v", err)
	}
	extra := cell.NewDict(32)
	if err = extra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreBigVarUInt(big.NewInt(777), 32).EndCell()); err != nil {
		t.Fatalf("failed to build extra currencies: %v", err)
	}

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled:     true,
		Bounce:          true,
		SrcAddr:         internalEmulationSrcAddr,
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(1000),
		ExtraCurrencies: extra,
		IHRFee:          tlb.FromNanoTONU(1),
		CreatedLT:       777,
		CreatedAt:       uint32(tonopsTestTime.Unix()) - 10,
		Body:            body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               versionCell,
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	out, err := res.Transaction.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to read outbound messages: %v", err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("unexpected outbound messages: %+v", out)
	}
	bounced := out[0].AsInternal()
	if got := bounced.IHRFee.Nano().Uint64(); got != 1 {
		t.Fatalf("bounce should preserve low extra_flags bits, got=%d", got)
	}
	bs := bounced.Body.MustBeginParse()
	tag, err := bs.LoadUInt(32)
	if err != nil {
		t.Fatalf("failed to load bounce body tag: %v", err)
	}
	if tag != 0xFFFFFFFE {
		t.Fatalf("unexpected bounce body tag: %x", tag)
	}
	if bs.RefsNum() != 2 {
		t.Fatalf("new bounce body should contain original body/info refs, got %d", bs.RefsNum())
	}
	if bounced.ExtraCurrencies == nil {
		t.Fatal("bounce should preserve extra currencies in message value")
	}
	desc, ok := res.Transaction.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("unexpected description type %T", res.Transaction.Description)
	}
	bounceOK, ok := desc.BouncePhase.Phase.(tlb.BouncePhaseOk)
	if !ok {
		t.Fatalf("unexpected bounce phase type %T", desc.BouncePhase.Phase)
	}
	if got := bounceOK.MsgFees.Nano().Uint64(); got != 51 {
		t.Fatalf("unexpected collected bounce fee: got=%d want=51", got)
	}
}

func TestEmulateTransactionLegacyBounceClearsExtraFlagsBeforeV12(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	shard := buildTransactionTestNoneShardAccount(t)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 11, Capabilities: 4})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: 100,
		CellPrice: 1 << 16,
		FirstFrac: 1 << 15,
	})
	if err != nil {
		t.Fatalf("failed to build msg forward prices config: %v", err)
	}

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1000),
		IHRFee:      tlb.FromNanoTONU(1),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	machine, err := NewTVM().WithGlobalVersion(11)
	if err != nil {
		t.Fatalf("failed to create v11 TVM: %v", err)
	}
	res, err := machine.EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               versionCell,
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction failed: %v", err)
	}

	out, err := res.Transaction.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to read outbound messages: %v", err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("unexpected outbound messages: %+v", out)
	}
	bounced := out[0].AsInternal()
	if got := bounced.IHRFee.Nano().Uint64(); got != 0 {
		t.Fatalf("legacy bounce should clear extra_flags before v12, got=%d", got)
	}
	bs := bounced.Body.MustBeginParse()
	tag, err := bs.LoadUInt(32)
	if err != nil {
		t.Fatalf("failed to load bounce body tag: %v", err)
	}
	if tag != 0xFFFFFFFF {
		t.Fatalf("unexpected legacy bounce body tag: %x", tag)
	}
	if bs.RefsNum() != 0 {
		t.Fatalf("legacy bounce body should not contain new bounce refs, got %d", bs.RefsNum())
	}
}

func TestTransactionBlackholeBurnsInboundGramsBeforeCreditPhase(t *testing.T) {
	blackhole := address.MustParseRawAddr("-1:1111111111111111111111111111111111111111111111111111111111111111")
	burningCell, err := tlb.ToCell(&tlb.BurningConfig{
		BlackholeAddr: blackhole.Data(),
	})
	if err != nil {
		t.Fatalf("failed to build burning config: %v", err)
	}

	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     blackhole,
			Amount:      tlb.FromNanoTONU(777),
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    blackhole,
		status:  tlb.AccountStatusActive,
		balance: big.NewInt(1000),
		storageInfo: tlb.StorageInfo{
			StorageExtra: tlb.StorageExtraNone{},
		},
	}
	cfg := transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamBurningConfig: burningCell,
		}),
	})

	prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(0), big.NewInt(0), uint32(tonopsTestTime.Unix()), cfg, transactionStorageDueLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.msgBalance.grams.Sign() != 0 {
		t.Fatalf("message balance grams = %s, want 0", prepared.msgBalance.grams)
	}
	if got := prepared.creditPhase.Credit.Coins.Nano(); got.Sign() != 0 {
		t.Fatalf("credit phase grams = %s, want 0", got)
	}
	if prepared.balance.Uint64() != 1000 {
		t.Fatalf("account balance = %s, want 1000", prepared.balance)
	}
}

func TestTransactionStorageDeletionUsesCreditedExtraCurrencies(t *testing.T) {
	extra := makeTransactionExtraCurrencies(t, 1, 777)
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled:     true,
			SrcAddr:         internalEmulationSrcAddr,
			DstAddr:         tonopsTestAddr,
			Amount:          tlb.FromNanoTONU(10),
			ExtraCurrencies: extra,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusUninit,
		balance: big.NewInt(50),
		storageInfo: tlb.StorageInfo{
			StorageExtra: tlb.StorageExtraNone{},
		},
	}

	prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(500), big.NewInt(0), uint32(tonopsTestTime.Unix()), transactionConfig{}, transactionStorageDueLimits{
		deleteDue: big.NewInt(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.deleted {
		t.Fatal("account with credited extra currencies should not be deleted")
	}
	if prepared.status != tlb.AccountStatusUninit {
		t.Fatalf("status = %s, want uninit", prepared.status)
	}
	if prepared.storagePhase.StatusChange.Type != tlb.AccStatusChangeUnchanged {
		t.Fatalf("status change = %+v, want unchanged", prepared.storagePhase.StatusChange)
	}
}

func TestTransactionStorageDeletionDestroyedStartsAtV13(t *testing.T) {
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
		},
	}

	for _, tc := range []struct {
		name          string
		version       uint32
		wantDestroyed bool
	}{
		{name: "v12", version: 12},
		{name: "v13", version: 13, wantDestroyed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusUninit,
				balance: big.NewInt(50),
				storageInfo: tlb.StorageInfo{
					StorageExtra: tlb.StorageExtraNone{},
				},
			}

			prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(500), big.NewInt(0), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, tc.version), transactionStorageDueLimits{
				deleteDue: big.NewInt(100),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !prepared.deleted {
				t.Fatal("storage phase should delete the account status")
			}
			if prepared.destroyed != tc.wantDestroyed {
				t.Fatalf("destroyed = %t, want %t", prepared.destroyed, tc.wantDestroyed)
			}
			if prepared.storagePhase.StatusChange.Type != tlb.AccStatusChangeDeleted {
				t.Fatalf("status change = %+v, want deleted", prepared.storagePhase.StatusChange)
			}
		})
	}
}

func TestTransactionExternalUnfreezeStartsAtV8(t *testing.T) {
	stateInit := &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr:   tonopsTestAddr,
			StateInit: stateInit,
		},
	}

	for _, tc := range []struct {
		name       string
		version    uint32
		wantSkip   bool
		wantReason tlb.ComputeSkipReasonType
	}{
		{name: "v7", version: 7, wantSkip: true, wantReason: tlb.ComputeSkipReasonBadState},
		{name: "v8", version: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:      tonopsTestAddr,
				status:    tlb.AccountStatusFrozen,
				stateHash: stateCell.Hash(),
			}
			_, _, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusFrozen, false, msg, false, transactionTestConfigWithGlobalVersion(t, tc.version))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantSkip {
				if skip != nil {
					t.Fatalf("skip reason = %+v, want nil", skip)
				}
				return
			}
			if skip == nil || skip.Type != tc.wantReason {
				t.Fatalf("skip reason = %+v, want %s", skip, tc.wantReason)
			}
		})
	}
}

func TestTransactionBounceErrorsWhenMessageBalanceCannotBeDebited(t *testing.T) {
	msgBalance := &transactionCurrencyBalance{
		grams: big.NewInt(100),
		extra: map[uint32]*big.Int{1: big.NewInt(1)},
	}
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(100),
		},
	}

	_, err := transactionPrepareBouncePhase(msg, big.NewInt(100), nil, msgBalance, big.NewInt(0), big.NewInt(0), uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), 0, transactionConfig{}, nil, nil, nil)
	if err == nil {
		t.Fatal("bounce phase should fail when account balance cannot cover message extra currencies")
	}
}

func TestTransactionSendActionValidatesStateInitLibraries(t *testing.T) {
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	cfg := transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: versionCell,
		}),
	})
	invalidLib := cell.NewDict(256)
	if err = invalidLib.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreBoolBit(true).EndCell()); err != nil {
		t.Fatalf("failed to build invalid library dict: %v", err)
	}
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		StateInit: &tlb.StateInit{
			Lib: invalidLib,
		},
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}

	apply := func(mode uint8) *transactionActionApplyResult {
		t.Helper()
		actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: msgCell})
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
		out, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	failed := apply(0)
	if failed.phase == nil || failed.phase.Success || failed.phase.ResultCode != 34 || failed.phase.SkippedActions != 0 {
		t.Fatalf("unexpected invalid library action phase: %+v", failed.phase)
	}
	skipped := apply(2)
	if skipped.phase == nil || !skipped.phase.Success || skipped.phase.ResultCode != 0 || skipped.phase.SkippedActions != 1 {
		t.Fatalf("unexpected skipped invalid library action phase: %+v", skipped.phase)
	}
}

func TestEmulateTransactionRejectsExternalInAnycastDestination(t *testing.T) {
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(1, []byte{0}))
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: dst,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	_, err = NewTVM().EmulateTransaction(buildTransactionTestNoneShardAccount(t), msgCell, TransactionEmulationConfig{
		Address: dst,
		Now:     uint32(tonopsTestTime.Unix()),
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: versionCell,
		}),
	})
	if err == nil {
		t.Fatal("external inbound message with anycast destination should be rejected")
	}
}

func makeTransactionExtraCurrencies(t *testing.T, id uint32, amount uint64) *cell.Dictionary {
	t.Helper()

	dict := cell.NewDict(32)
	if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(id)), cell.BeginCell().MustStoreBigVarUInt(new(big.Int).SetUint64(amount), 32).EndCell()); err != nil {
		t.Fatalf("failed to build extra currencies: %v", err)
	}
	return dict
}
