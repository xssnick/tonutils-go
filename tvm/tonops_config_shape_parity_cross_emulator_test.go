//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

// Witnesses from the 2026-08 tonops parity audit: config-shaped inputs that
// used to diverge — a flat-pricing wrapper around a plain gas-prices record,
// a non-slice storage-prices slot and the always-present unpacked config
// tuple.

func assertTonopsC7ParityCase(t *testing.T, name string, code *cell.Cell, stackVals []any, c7 tuple.Tuple, configRoot *cell.Cell, version int) {
	t.Helper()

	goStack, err := buildCrossStack(stackVals...)
	if err != nil {
		t.Fatalf("%s: go stack: %v", name, err)
	}
	refStack, err := buildCrossStack(stackVals...)
	if err != nil {
		t.Fatalf("%s: ref stack: %v", name, err)
	}
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, data, c7, nil, goStack, version, referenceDefaultMaxGas)
	if err != nil {
		t.Fatalf("%s: go run: %v", name, err)
	}

	refCfg := *tonopsCrossRefConfig(configRoot)
	refCfg.GasLimit = referenceDefaultMaxGas
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, refStack, refCfg)
	if err != nil {
		t.Fatalf("%s: reference run: %v", name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("%s: exit mismatch: go=%d reference=%d", name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("%s: gas mismatch: go=%d reference=%d", name, goRes.gasUsed, refRes.gasUsed)
	}
	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("%s: normalize go stack: %v", name, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("%s: normalize reference stack: %v", name, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("%s: stack mismatch:\ngo=%s\nreference=%s", name, goStackCell.Dump(), refStackCell.Dump())
	}
}

// The flat-pricing wrapper is only defined around the extended gas-prices
// record: wrapping the plain record instead is not a valid config and the fee
// opcodes fail with a cell underflow.
func TestTVMCrossEmulatorGasPricesFlatWrapperParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	flatPlain := cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(100, 64).      // flat_gas_limit
		MustStoreUInt(40000, 64).    // flat_gas_price
		MustStoreUInt(0xDD, 8).      // plain record (invalid inside the wrapper)
		MustStoreUInt(26214400, 64). // gas_price
		MustStoreUInt(1000000, 64).  // gas_limit
		MustStoreUInt(10000, 64).    // gas_credit
		MustStoreUInt(10000000, 64). // block_gas_limit
		MustStoreUInt(1000, 64).     // freeze_due_limit
		MustStoreUInt(1000, 64).     // delete_due_limit
		ToSlice()

	flatExt := cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(100, 64).
		MustStoreUInt(40000, 64).
		MustStoreUInt(0xDE, 8). // extended record: the valid pairing
		MustStoreUInt(26214400, 64).
		MustStoreUInt(1000000, 64).
		MustStoreUInt(1000000, 64). // special_gas_limit
		MustStoreUInt(10000, 64).
		MustStoreUInt(10000000, 64).
		MustStoreUInt(1000, 64).
		MustStoreUInt(1000, 64).
		ToSlice()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13})
	if err != nil {
		t.Fatalf("build global version: %v", err)
	}
	for _, tc := range []struct {
		name   string
		prices *cell.Slice
	}{
		{name: "flat-plain-invalid", prices: flatPlain},
		{name: "flat-ext-valid", prices: flatExt},
	} {
		pricesCell, err := tc.prices.ToCell()
		if err != nil {
			t.Fatalf("%s: build prices cell: %v", tc.name, err)
		}
		configRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
			uint32(tlb.ConfigParamGlobalVersion):        versionCell,
			uint32(tlb.ConfigParamGasPricesMasterchain): pricesCell,
			uint32(tlb.ConfigParamGasPricesBasechain):   pricesCell,
		})
		unpacked := tuple.NewTupleSized(7)
		mustSetTupleValue(t, &unpacked, 2, tc.prices)
		mustSetTupleValue(t, &unpacked, 3, tc.prices)
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{ConfigRoot: configRoot, UnpackedConfig: unpacked})
		code := prependRawMethodDrop(cell.BeginCell().MustStoreUInt(0xF836, 16).EndCell())
		assertTonopsC7ParityCase(t, tc.name, code, []any{int64(1000), int64(0)}, c7, configRoot, 13)
	}
}

// GETSTORAGEFEE reads slot 0 as a slice: any other value means "no active
// storage prices" and pushes 0.
func TestTVMCrossEmulatorUnpackedConfigSlotShapeParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configRoot := tonopsCrossConfigWithGlobalVersion(t, 13)

	// GETSTORAGEFEE with an int in slot 0.
	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 0, big.NewInt(42))
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{ConfigRoot: configRoot, UnpackedConfig: unpacked})
	code := prependRawMethodDrop(cell.BeginCell().MustStoreUInt(0xF837, 16).EndCell())
	assertTonopsC7ParityCase(t, "storagefee-non-slice-slot", code,
		[]any{int64(10), int64(20), int64(3), int64(0)}, c7, configRoot, 13)

	// An empty unpacked config tuple is a 7-null tuple on both engines.
	emptyUnpacked := tuple.NewTupleSized(7)
	c7Empty := makeTonopsTestC7(t, tonopsTestC7Config{ConfigRoot: configRoot, UnpackedConfig: emptyUnpacked})
	assertTonopsC7ParityCase(t, "storagefee-null-slot", code,
		[]any{int64(10), int64(20), int64(3), int64(0)}, c7Empty, configRoot, 13)
}

// BLS_PAIRING with nothing aggregated (n=0, or every pair being two points at
// infinity) reports false rather than the identity check's true.
func TestTVMCrossEmulatorBLSPairingEmptyAggregateParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	g1Inf := make([]byte, 48)
	g1Inf[0] = 0xC0
	g2Inf := make([]byte, 96)
	g2Inf[0] = 0xC0

	configRoot := tonopsCrossConfigWithGlobalVersion(t, 13)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{ConfigRoot: configRoot})
	code := prependRawMethodDrop(cell.BeginCell().MustStoreUInt(0xF93030, 24).EndCell())

	assertTonopsC7ParityCase(t, "bls-pairing-zero-pairs", code, []any{int64(0)}, c7, configRoot, 13)

	infPair := []any{
		cell.BeginCell().MustStoreSlice(g1Inf, 48*8).ToSlice(),
		cell.BeginCell().MustStoreSlice(g2Inf, 96*8).ToSlice(),
		int64(1),
	}
	assertTonopsC7ParityCase(t, "bls-pairing-infinity-pair", code, infPair, c7, configRoot, 13)
}

// Running out of gas on the cell-create inside HASHSU must surface as regular
// unhandled out-of-gas, not a fatal panic.
func TestTVMCrossEmulatorHashSUOutOfGasParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := rawCodeCellFromHex(t, "30F901") // DROP; HASHSU
	payload := rawCodeCellFromHex(t, "AA").MustBeginParse()
	for _, limit := range []int64{100, 300, 499, 526, 600, 1000} {
		assertFlowParityCase(t, "hashsu-oog", code, []any{payload.Copy()}, 13, limit)
	}
}

// The rewritten address is finalized into a cell and loaded back, so the load
// is deduplicated by hash: a repeated identical REWRITEVARADDR pays the reload
// price, and the registered hash cheapens a later load of the same cell.
func TestTVMCrossEmulatorRewriteVarAddrCellAccountingParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// addr_var$11 anycast(depth=5, pfx=10101) addr_len=20 wc=0 addr=20 bits
	varAddr := func() *cell.Slice {
		return cell.BeginCell().
			MustStoreUInt(0b11, 2).
			MustStoreBoolBit(true).     // has anycast
			MustStoreUInt(5, 5).        // depth
			MustStoreUInt(0b10101, 5).  // rewrite prefix
			MustStoreUInt(20, 9).       // addr_len
			MustStoreUInt(0, 32).       // workchain
			MustStoreUInt(0xABCDE, 20). // address
			ToSlice()
	}

	// DROP; REWRITEVARADDR; DROP; DROP  (single rewrite)
	single := rawCodeCellFromHex(t, "30FA463030")
	assertFlowParityCase(t, "rewritevaraddr-single", single, []any{varAddr()}, 13, 1_000_000)

	// DROP; REWRITEVARADDR; DROP; DROP; REWRITEVARADDR; DROP; DROP
	// the second rewrite of the same address must hit the reload price
	twice := rawCodeCellFromHex(t, "30FA463030FA463030")
	assertFlowParityCase(t, "rewritevaraddr-twice", twice, []any{varAddr(), varAddr()}, 13, 1_000_000)

	// Gas sweep around the create/load boundary pins the charge order too.
	for _, limit := range []int64{60, 500, 560, 660, 1_000} {
		assertFlowParityCase(t, "rewritevaraddr-oog", single, []any{varAddr()}, 13, limit)
	}
}

// The cell-create charge happens before the cell is built, so a depth-limit
// failure still costs it and an exhausted meter reports out-of-gas instead of
// a cell overflow. The deep cell is built inside the VM: the reference harness
// cannot serialize one through the stack.
func TestTVMCrossEmulatorActionCellCreateChargeOrderParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// DROP; NEWC; ENDC; PUSHINT16 1024; PUSHCONT{NEWC;STREF;ENDC}; REPEAT;
	// PUSHINT 0; SENDRAWMSG  -> action cell would reach depth 1025
	deepAction := rawCodeCellFromHex(t, "30C8C981040093C8CCC9E470FB00")
	for _, limit := range []int64{1_000_000, 590_000, 585_000, 580_000} {
		assertFlowParityCase(t, "sendrawmsg-deep-action", deepAction, nil, 13, limit)
	}

	// RAWRESERVE builds its action cell the same way; sweeping the limit over
	// the create charge pins the order for the succeeding path too.
	reserve := rawCodeCellFromHex(t, "30FB02")
	for _, limit := range []int64{200, 499, 526, 600, 1_000_000} {
		assertFlowParityCase(t, "rawreserve-charge", reserve, []any{int64(100), int64(0)}, 13, limit)
	}
}
