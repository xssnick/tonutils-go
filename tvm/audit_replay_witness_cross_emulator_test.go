//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// This file carries the replay witnesses from the external v1.18.0 parity
// audit, inverted into permanent regression tests: every case that used to
// pin a divergence now requires both emulators (or the fixed Go API contract)
// to agree with the reference behavior.

// RW-11: the immediate-shift constructors take the full 1..256 range and the
// encoded 8-bit suffix stores shift-1.
func TestAuditReplayWitnessImmediateShiftConstructorRange(t *testing.T) {
	for shift, want := range map[int]uint64{1: 0xAA00, 127: 0xAA7E, 128: 0xAA7F, 256: 0xAAFF} {
		raw := mathop.LSHIFTCODE(shift).Serialize().EndCell().MustBeginParse().MustLoadUInt(16)
		if raw != want {
			t.Fatalf("LSHIFT# %d encoding = %#x, want %#x", shift, raw, want)
		}
	}
}

// RW-12: StoreBigInt enforces the signed range; +128 does not fit 8 signed
// bits, while the boundaries +127 and -128 round-trip.
func TestAuditReplayWitnessSignedBuilderBoundary(t *testing.T) {
	if err := cell.BeginCell().StoreBigInt(big.NewInt(128), 8); !errors.Is(err, cell.ErrTooBigValue) {
		t.Fatalf("StoreBigInt(+128,8) error = %v, want ErrTooBigValue", err)
	}
	for _, v := range []int64{127, -128} {
		b := cell.BeginCell()
		if err := b.StoreBigInt(big.NewInt(v), 8); err != nil {
			t.Fatalf("StoreBigInt(%d,8) failed: %v", v, err)
		}
		got, err := b.EndCell().MustBeginParse().LoadBigInt(8)
		if err != nil || got.Int64() != v {
			t.Fatalf("signed round trip of %d = %v (err=%v)", v, got, err)
		}
	}
}

// RW-13: a Merkle-update boundary pair with equal stored hashes but different
// stored depths is rejected, mirroring the reference compare_cells contract.
func TestAuditReplayWitnessMerkleUpdateDepthComparison(t *testing.T) {
	const h32 = "0000000000000000000000000000000000000000000000000000000000000000"
	encoded, err := hex.DecodeString(
		"b5ee9c7201010301009500" +
			"0a8a04" + h32 + h32 + "000000010102" +
			"28480101" + h32 + "0000" +
			"28480101" + h32 + "0001",
	)
	if err != nil {
		t.Fatalf("failed to decode witness BOC: %v", err)
	}
	update, err := cell.FromBOC(encoded)
	if err != nil {
		t.Fatalf("witness BOC must parse before Merkle validation: %v", err)
	}
	if err = cell.ValidateMerkleUpdate(update); err == nil {
		t.Fatal("equal-hash/different-depth Merkle boundary was accepted, want rejection")
	}
}

// RW-14: a library cell whose BOC descriptor claims level mask 1 contradicts
// the structurally derived mask 0 and is rejected by the default parser.
func TestAuditReplayWitnessLibraryLevelMaskComparison(t *testing.T) {
	const h32 = "0000000000000000000000000000000000000000000000000000000000000000"
	encoded, err := hex.DecodeString("b5ee9c7201010101002300284202" + h32)
	if err != nil {
		t.Fatalf("failed to decode witness BOC: %v", err)
	}
	if _, err = cell.FromBOC(encoded); err == nil {
		t.Fatal("library cell with descriptor level mask 1 was accepted, want rejection")
	}
}

// RW-01: an active account without code runs the VM and fails with the fatal
// exit code on both emulators.
func TestAuditReplayWitnessActiveAccountWithoutCode(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = 14
	now := uint32(tonopsTestTime.Unix())
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), version)
	shard := buildTransactionTestShardAccount(
		t,
		tonopsTestAddr,
		nil,
		cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell(),
		2_000_000_000,
		now,
	)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, refRes := auditReplayWitnessRunBoth(t, shard, msg, now, configRoot)

	goVM, goOK := mustTransactionComputePhase(t, goRes.TransactionCell).Phase.(tlb.ComputePhaseVM)
	refVM, refOK := mustTransactionComputePhase(t, refRes.txCell).Phase.(tlb.ComputePhaseVM)
	if !goOK || !refOK || goVM.Details.ExitCode != ^int32(vmerr.CodeFatal) || refVM.Details.ExitCode != ^int32(vmerr.CodeFatal) {
		t.Fatalf("unexpected compute phases: goOK=%t refOK=%t goExit=%d refExit=%d", goOK, refOK, goVM.Details.ExitCode, refVM.Details.ExitCode)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

// RW-02: a present gas config with a literal zero limit skips the compute
// phase with NO_GAS on both emulators.
func TestAuditReplayWitnessConfiguredZeroGasLimit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = 14
	now := uint32(tonopsTestTime.Unix())
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasPrice:        1,
		GasLimit:        0,
		SpecialGasLimit: 0,
		GasCredit:       0,
		BlockGasLimit:   100_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build zero-limit gas config: %v", err)
	}
	baseConfig := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), version)
	configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfig, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
	})
	code := makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), 2_000_000_000, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, refRes := auditReplayWitnessRunBoth(t, shard, msg, now, configRoot)

	goSkipped, goOK := mustTransactionComputePhase(t, goRes.TransactionCell).Phase.(tlb.ComputePhaseSkipped)
	refSkipped, refOK := mustTransactionComputePhase(t, refRes.txCell).Phase.(tlb.ComputePhaseSkipped)
	if !goOK || !refOK || goSkipped.Reason.Type != tlb.ComputeSkipReasonNoGas || refSkipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("unexpected zero-limit compute phases: goOK=%t refOK=%t", goOK, refOK)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

// RW-03: an accepted external message on a special account serializes the
// special gas limit on both emulators from global version 5 on.
func TestAuditReplayWitnessSpecialExternalSerializedGasLimit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const (
		version      = 14
		specialLimit = uint64(1_000)
	)
	now := uint32(tonopsTestTime.Unix())
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x42}, 32))
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build special address config: %v", err)
	}
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            10,
		FlatGasPrice:            100,
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		SpecialGasLimit:         specialLimit,
		GasCredit:               100,
		BlockGasLimit:           1_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas config: %v", err)
	}
	baseConfig := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), version)
	configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfig, map[int32]*cell.Cell{
		int32(tlb.ConfigParamConfigAddress):        configAddrCell,
		int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
	})
	code := makeTransactionExternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	shard := buildTransactionTestShardAccount(t, specialAddr, code, cell.BeginCell().EndCell(), 2_000_000_000, now)
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: specialAddr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	goRes, err := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
		Address:     specialAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goVM, goOK := mustTransactionComputePhase(t, goRes.TransactionCell).Phase.(tlb.ComputePhaseVM)
	refVM, refOK := mustTransactionComputePhase(t, refRes.txCell).Phase.(tlb.ComputePhaseVM)
	if !goOK || !refOK || goVM.Details.GasLimit.Uint64() != specialLimit || refVM.Details.GasLimit.Uint64() != specialLimit {
		t.Fatalf("unexpected external special gas limits: go=%v reference=%v", goVM.Details.GasLimit, refVM.Details.GasLimit)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

// RW-04: before extra_currency_v2 (gv10) send mode 64 carries the complete
// remaining inbound CurrencyCollection, extra currencies included.
func TestAuditReplayWitnessLegacyMode64ExtraCurrencies(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = 9
	now := uint32(tonopsTestTime.Unix())
	extra := makeTransactionExtraCurrencies(t, 7, 11)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled:     true,
		SrcAddr:         internalEmulationSrcAddr,
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(1_000_000_000),
		ExtraCurrencies: extra,
		Body:            cell.BeginCell().EndCell(),
	})
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{
		Mode: 64,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	})
	code := makeTransactionInternalActionsCode(t, actions, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), 2_000_000_000, now)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
	baseConfig := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), version)
	configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfig, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})

	goRes, refRes := auditReplayWitnessRunBoth(t, shard, msg, now, configRoot)

	goExtra := auditReplayWitnessSingleOutExtra(t, goRes.TransactionCell)
	refExtra := auditReplayWitnessSingleOutExtra(t, refRes.txCell)
	if goExtra[7] == nil || goExtra[7].Uint64() != 11 || refExtra[7] == nil || refExtra[7].Uint64() != 11 {
		t.Fatalf("unexpected mode64 extra currencies: go=%v reference=%v", goExtra, refExtra)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

// RW-15: the default external-message helper keeps the explicit zero limit,
// starting with only the credit as budget like the reference GasLimits.
func TestAuditReplayWitnessExternalDefaultGasLimit(t *testing.T) {
	gas := defaultExternalMessageGas(vmcore.Gas{})
	if gas.Limit != 0 {
		t.Fatalf("unexpected limit: got=%d want=0", gas.Limit)
	}
	if gas.Max != DefaultExternalMessageGasMax {
		t.Fatalf("unexpected max: got=%d want=%d", gas.Max, DefaultExternalMessageGasMax)
	}
	if gas.Remaining != DefaultExternalMessageGasCredit || gas.Credit != DefaultExternalMessageGasCredit {
		t.Fatalf("unexpected starting budget: remaining=%d credit=%d want=%d", gas.Remaining, gas.Credit, DefaultExternalMessageGasCredit)
	}
}

// RW-05: a library cell resolving to another library cell is permitted before
// global version 5 and rejected from version 5 on, by both emulators.
func TestAuditReplayWitnessPreV5NestedLibrary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	inner := mustCrossLibraryCellForHash(t, target.Hash())
	outer := mustCrossLibraryCellForHash(t, inner.Hash())
	libraries := mustCrossLibraryCollection(t, target, inner)
	code := prependRawMethodDrop(codeFromBuilders(t, cellsliceop.CTOS().Serialize()))

	for _, tc := range []struct {
		version  int
		wantExit int32
	}{
		{version: 4, wantExit: 0},
		{version: 5, wantExit: int32(vmerr.CodeCellUnderflow)},
	} {
		goStack, err := buildCrossStack(outer)
		if err != nil {
			t.Fatalf("failed to build go stack: %v", err)
		}
		goRes, err := runGoCrossCodeWithVersionAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, []*cell.Cell{libraries}, goStack, tc.version)
		if err != nil {
			t.Fatalf("go execution failed: %v", err)
		}

		refStack, err := buildCrossStack(outer)
		if err != nil {
			t.Fatalf("failed to build reference stack: %v", err)
		}
		refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(tc.version)))
		refCfg.Libs = libraries
		refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
		if err != nil {
			t.Fatalf("reference execution failed: %v", err)
		}

		if goRes.exitCode != tc.wantExit || refRes.exitCode != tc.wantExit {
			t.Fatalf("v%d nested-library result: go=%d reference=%d want=%d", tc.version, goRes.exitCode, refRes.exitCode, tc.wantExit)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("v%d nested-library gas mismatch: go=%d reference=%d", tc.version, goRes.gasUsed, refRes.gasUsed)
		}
	}
}

// RW-06: legacy (pre-gv4) library lookup charges gas for every dictionary
// node the lookup loads, matching the reference.
func TestAuditReplayWitnessPreV4LibraryCollectionPathGas(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
	entries := []*cell.Cell{target}
	for i := uint64(0); i < 12; i++ {
		entries = append(entries, cell.BeginCell().MustStoreUInt(0xA000+i, 16).EndCell())
	}
	libraries := mustCrossLibraryCollection(t, entries...)
	code := prependRawMethodDrop(libraryLookupGasCode(t, 1))

	for _, version := range []int{3, 4} {
		goRes, refRes := runLibraryLookupGasCrossCase(t, version, code, []any{libraryCell}, libraries)
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("v%d library lookup gas mismatch: go=%d reference=%d", version, goRes.gasUsed, refRes.gasUsed)
		}
	}
}

// RW-07: the IHR component of SENDMSG is floored on both emulators.
func TestAuditReplayWitnessSendMsgIHRRounding(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = 10
	prices := tlb.ConfigMsgForwardPrices{
		LumpPrice: 1,
		IHRFactor: 1,
	}
	configRoot := tonopsCrossSendMsgConfig(t, version, prices)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
	})
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: false,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(0),
		CreatedLT:   1,
		CreatedAt:   uint32(tonopsTestTime.Unix()),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build message: %v", err)
	}
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))

	goStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go execution failed: %v", err)
	}
	refStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *tonopsCrossRefConfig(configRoot))
	if err != nil {
		t.Fatalf("reference execution failed: %v", err)
	}

	goValues := auditReplayWitnessStackValues(t, goRes.stack)
	refValues := auditReplayWitnessStackValues(t, refRes.stack)
	if len(goValues) != 1 || len(refValues) != 1 {
		t.Fatalf("unexpected result stack sizes: go=%d reference=%d", len(goValues), len(refValues))
	}
	goFee, goOK := goValues[0].(*big.Int)
	refFee, refOK := refValues[0].(*big.Int)
	if !goOK || !refOK || goFee.Int64() != 1 || refFee.Int64() != 1 {
		t.Fatalf("unexpected SENDMSG fees: go=%v reference=%v (want 1)", goValues[0], refValues[0])
	}
}

// RW-08: after a successful send followed by a failed action at gv14 both
// emulators replace total_action_fees with the (zero) action fine.
func TestAuditReplayWitnessFailedActionFee(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 14)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1)
	actions := buildTransactionActionList(t,
		tlb.ActionSendMsg{Mode: 1, Msg: outMsg},
		tlb.ActionSendMsg{Mode: 0xC0, Msg: outMsg},
	)
	code := auditReplayWitnessReturnActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 2_000_000_000, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, refRes := auditReplayWitnessRunBoth(t, shard, msg, now, configRoot)

	goCode, goFee := auditReplayWitnessActionResult(t, goRes.TransactionCell)
	refCode, refFee := auditReplayWitnessActionResult(t, refRes.txCell)
	if goCode != 34 || refCode != 34 {
		t.Fatalf("unexpected action result codes: go=%d reference=%d", goCode, refCode)
	}
	if goFee.Sign() != 0 || refFee.Sign() != 0 {
		t.Fatalf("unexpected total action fees: go=%s reference=%s, want both zero", goFee, refFee)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

// RW-09: a config containing only the global-version parameter is rejected by
// both transaction loaders.
func TestAuditReplayWitnessCriticalConfigFailOpen(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, 14),
	})
	if _, err := PrepareBlockchainConfig(configRoot); err == nil {
		t.Fatal("go accepted a config without gas/forward prices, want a preparation error")
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 2_000_000_000, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	if _, err := runReferenceOrdinaryTransactionWithConfigRoot(
		shard,
		msg,
		now,
		uint64(transactionTestLogicalTime),
		tonopsTestSeed,
		configRoot,
	); err == nil {
		t.Fatal("reference transaction loader unexpectedly accepted the partial config")
	}
}

// RW-10: an outbound message that stops fitting into a cell after the source
// rewrite fails the action phase with result 39 on both emulators.
func TestAuditReplayWitnessOutboundRewriteDoesNotFit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const workchain = int32(100)
	now := uint32(tonopsTestTime.Unix())
	workchainsCell := auditReplayWitnessExtendedWorkchainsCell(t, workchain, 511)
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{LumpPrice: 400_000})
	if err != nil {
		t.Fatalf("failed to build forwarding prices: %v", err)
	}
	baseConfig := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 14)
	configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfig, map[int32]*cell.Cell{
		int32(tlb.ConfigParamWorkchains):                  workchainsCell,
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})

	dstData := bytes.Repeat([]byte{0xA5}, 64)
	dstData[len(dstData)-1] &= 0xFE
	dst := address.NewAddressVar(0, workchain, 511, dstData)
	transferAmount := new(big.Int).Lsh(big.NewInt(1), 63)
	outMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     dst,
		Amount:      tlb.FromNanoTON(transferAmount),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build large outbound message: %v", err)
	}
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: outMsg})
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := auditReplayWitnessReturnActionsCode(t, actions, newData)
	largeBalance := new(big.Int).Lsh(big.NewInt(1), 100)
	shard := auditReplayWitnessBigBalanceShardAccount(t, tonopsTestAddr, code, origData, largeBalance, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTON(largeBalance),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, refRes := auditReplayWitnessRunBoth(t, shard, msg, now, configRoot)

	goCode, _ := auditReplayWitnessActionResult(t, goRes.TransactionCell)
	refCode, _ := auditReplayWitnessActionResult(t, refRes.txCell)
	if goCode != 39 || refCode != 39 {
		t.Fatalf("unexpected rewrite-overflow result codes: go=%d reference=%d, want 39", goCode, refCode)
	}
	auditReplayWitnessRequireEqualTx(t, goRes.TransactionCell, refRes.txCell)
}

func auditReplayWitnessRunBoth(t *testing.T, shard *tlb.ShardAccount, msg *cell.Cell, now uint32, configRoot *cell.Cell) (*TransactionExecutionResult, *referenceTransactionResult) {
	t.Helper()

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}
	return goRes, refRes
}

func auditReplayWitnessRequireEqualTx(t *testing.T, goTx *cell.Cell, refTx *cell.Cell) {
	t.Helper()
	if !bytes.Equal(goTx.Hash(), refTx.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goTx), transactionCrossTxSummary(t, refTx))
	}
}

func auditReplayWitnessSingleOutExtra(t *testing.T, txCell *cell.Cell) map[uint32]*big.Int {
	t.Helper()
	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}
	if tx.OutMsgCount != 1 || tx.IO.Out == nil {
		t.Fatalf("unexpected outbound message count: %d", tx.OutMsgCount)
	}
	out, err := tx.IO.Out.ToSlice()
	if err != nil || len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("failed to decode outbound internal message: count=%d err=%v", len(out), err)
	}
	extra, err := transactionLoadExtraCurrencies(out[0].AsInternal().ExtraCurrencies)
	if err != nil {
		t.Fatalf("failed to decode outbound extra currencies: %v", err)
	}
	return extra
}

func auditReplayWitnessStackValues(t *testing.T, stackCell *cell.Cell) []any {
	t.Helper()

	var stack tlb.Stack
	if err := tlb.Parse(&stack, stackCell); err != nil {
		t.Fatalf("failed to parse result stack: %v", err)
	}

	values := make([]any, 0, stack.Depth())
	for stack.Depth() > 0 {
		value, err := stack.Pop()
		if err != nil {
			t.Fatalf("failed to pop result stack: %v", err)
		}
		values = append(values, value)
	}
	return values
}

func auditReplayWitnessActionResult(t *testing.T, txCell *cell.Cell) (int32, *big.Int) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok || desc.ActionPhase == nil {
		t.Fatalf("transaction has no ordinary action phase: %T", tx.Description)
	}
	fee := new(big.Int)
	if desc.ActionPhase.TotalActionFees != nil {
		fee.Set(desc.ActionPhase.TotalActionFees.Nano())
	}
	return desc.ActionPhase.ResultCode, fee
}

func auditReplayWitnessReturnActionsCode(t *testing.T, actions, newData *cell.Cell) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(actions).Serialize(),
		execop.POPCTR(5).Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func auditReplayWitnessExtendedWorkchainsCell(t *testing.T, workchain int32, addrBits uint16) *cell.Cell {
	t.Helper()
	descrCell, err := tlb.ToCell(&tlb.WorkchainDescr{
		Descr: tlb.WorkchainDescrV1{
			WorkchainDescrFields: tlb.WorkchainDescrFields{
				Active:            true,
				AcceptMsgs:        true,
				ZeroStateRootHash: make([]byte, 32),
				ZeroStateFileHash: make([]byte, 32),
				Format: tlb.WorkchainFormatExtended{
					MinAddrLen:      addrBits,
					MaxAddrLen:      addrBits,
					AddrLenStep:     1,
					WorkchainTypeID: 1,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build extended workchain descriptor: %v", err)
	}
	workchains := cell.NewDict(32)
	if err = workchains.SetIntKey(big.NewInt(int64(workchain)), descrCell); err != nil {
		t.Fatalf("failed to store extended workchain descriptor: %v", err)
	}
	root, err := tlb.ToCell(&tlb.WorkchainsConfig{Workchains: workchains})
	if err != nil {
		t.Fatalf("failed to build workchains config: %v", err)
	}
	return root
}

func auditReplayWitnessBigBalanceShardAccount(t *testing.T, addr *address.Address, code, data *cell.Cell, balance *big.Int, lastPaid uint32) *tlb.ShardAccount {
	t.Helper()
	storageInfoCell, err := tlb.ToCell(&tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     lastPaid,
	})
	if err != nil {
		t.Fatalf("failed to serialize storage info: %v", err)
	}
	stateInitCell, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}
	accountCell := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreUInt(0, 64).
		MustStoreBigCoins(balance).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()
	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
	}
}
