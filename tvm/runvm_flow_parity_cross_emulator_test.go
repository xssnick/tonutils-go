//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// Witnesses from the 2026-08 execution-flow parity audit: every case used to
// diverge between the Go VM and the reference emulator (child VM exit codes,
// quit-continuation semantics, pass-all call gas, pre-v4 terminal gas checks,
// get-method auto-commit) and now requires both emulators to agree exactly.

func assertFlowParityCase(t *testing.T, name string, code *cell.Cell, stackVals []any, globalVersion int, gasLimit int64) {
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

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, data, tuple.Tuple{}, nil, goStack, globalVersion, gasLimit)
	if err != nil {
		t.Fatalf("%s: go run: %v", name, err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = gasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, refStack, *refCfg)
	if err != nil {
		t.Fatalf("%s: reference run: %v", name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("%s: exit mismatch: go=%d reference=%d", name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("%s: gas mismatch: go=%d reference=%d", name, goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("%s: stack mismatch:\ngo:  %s\nref: %s", name, goRes.stack.Dump(), refRes.stack.Dump())
	}
}

func TestTVMCrossEmulatorChildVMExitCodeParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// A child THROW 13 caught by the default c2 is a handled exit: the parent
	// receives +13, not -14 (which is reserved for genuine unhandled
	// out-of-gas so contracts cannot fake it).
	t.Run("child_throw13_reports_positive", func(t *testing.T) {
		code := rawCodeCellFromHex(t, "30DB4000") // DROP; RUNVM 0
		child := rawCodeCellFromHex(t, "F20D").MustBeginParse()
		assertFlowParityCase(t, "throw13", code, []any{int64(0), child}, 13, 1_000_000)
	})

	// A child jumping into its default quit continuation with code 11
	// terminates directly: at most one stack value is returned, no exception
	// gas is charged and c2 is never involved.
	t.Run("child_quit11_direct_exit", func(t *testing.T) {
		code := rawCodeCellFromHex(t, "30DB4000")                 // DROP; RUNVM 0
		child := rawCodeCellFromHex(t, "ED43D9").MustBeginParse() // PUSHCTR c3; JMPX
		assertFlowParityCase(t, "quit11-stack", code, []any{int64(42), int64(1), child}, 13, 1_000_000)
		child2 := rawCodeCellFromHex(t, "ED43D9").MustBeginParse()
		assertFlowParityCase(t, "quit11-empty", code, []any{int64(0), child2}, 13, 1_000_000)
	})

	// RUNVM +4 with a non-committing child: below v11 the parent receives the
	// degenerate committed-state entry (typed as a cell but holding a null
	// reference) — CTOS on it fails the type check, ISNULL yields false;
	// since v11 it receives null.
	t.Run("ret_data_non_committed_child", func(t *testing.T) {
		ctos := rawCodeCellFromHex(t, "30DB4004D0") // DROP; RUNVM 4; CTOS
		child := rawCodeCellFromHex(t, "F20D").MustBeginParse()
		dataCell := rawCodeCellFromHex(t, "AB")
		assertFlowParityCase(t, "ret4-gv10-ctos", ctos, []any{int64(0), child, dataCell}, 10, 1_000_000)

		isnull := rawCodeCellFromHex(t, "30DB40046E") // DROP; RUNVM 4; ISNULL
		for _, version := range []int{10, 11} {
			child = rawCodeCellFromHex(t, "F20D").MustBeginParse()
			dataCell = rawCodeCellFromHex(t, "AB")
			assertFlowParityCase(t, "ret4-isnull", isnull, []any{int64(0), child, dataCell}, version, 1_000_000)
		}

		// CONDSELCHK sees the degenerate entry as a cell: same type as an
		// ordinary cell, so selection succeeds and only the later CTOS trips.
		condsel := rawCodeCellFromHex(t, "30DB4004C8C9E305D0") // DROP; RUNVM 4; NEWC; ENDC; CONDSELCHK; CTOS
		child = rawCodeCellFromHex(t, "F20D").MustBeginParse()
		dataCell = rawCodeCellFromHex(t, "AB")
		assertFlowParityCase(t, "ret4-condselchk", condsel, []any{int64(0), child, dataCell}, 10, 1_000_000)
	})
}

func TestTVMCrossEmulatorPassAllCallGasParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// a call that passes the whole stack moves it over without a stack-depth
	// charge, so a deep stack costs the same as a shallow one
	code := rawCodeCellFromHex(t, "30907F7FDB38") // DROP; PUSHCONT{}; PUSHINT -1; PUSHINT -1; CALLXVARARGS
	for _, depth := range []int{5, 30, 40, 100} {
		vals := make([]any, depth)
		for i := range vals {
			vals[i] = int64(0)
		}
		assertFlowParityCase(t, "callxvarargs-passall", code, vals, 13, 1_000_000)
	}

	// The closure path (BLESSARGS 0,-1; EXECUTE) hits the same pass-all branch.
	bless := rawCodeCellFromHex(t, "308B08EE0FD8") // DROP; PUSHSLICE x{}; BLESSARGS 0,-1; EXECUTE
	deep := make([]any, 40)
	for i := range deep {
		deep[i] = int64(0)
	}
	assertFlowParityCase(t, "blessargs-passall", bless, deep, 13, 1_000_000)
}

func TestTVMCrossEmulatorPreV4TerminalGasCheckParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Pre-v4 consumption is unchecked, but the post-step gas check still
	// catches an overdraft on the terminating instruction: the run ends with
	// unhandled out-of-gas (-14, stack holding the consumed gas), not success.
	ret := rawCodeCellFromHex(t, "30DB30") // DROP; RET
	for _, version := range []int{0, 2, 3, 4, 13} {
		assertFlowParityCase(t, "terminal-ret-overdraft", ret, nil, version, 40)
	}

	// Mid-program overdraft is caught right after the offending step too.
	drops := rawCodeCellFromHex(t, "3030") // DROP; DROP
	assertFlowParityCase(t, "midstep-overdraft", drops, []any{int64(0), int64(0)}, 3, 20)

	// Pre-v4 there is no mid-instruction gas check either: LDREFRTOS whose
	// instruction gas already overdrafts still loads the child (charging its
	// 100) before the post-step check reports out-of-gas, so the reported
	// gas_used includes the child load on both engines.
	withRef := cell.BeginCell().MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).EndCell().MustBeginParse()
	ldrefrtos := rawCodeCellFromHex(t, "30D5") // DROP; LDREFRTOS
	assertFlowParityCase(t, "ldrefrtos-midop-gv2", ldrefrtos, []any{withRef}, 2, 30)
}

func TestTVMCrossEmulatorAutoCommitGetMethodParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// The reference get-method flow still runs the final automatic commit: a
	// successful run leaving a >512-deep c4 fails with an inverted cell
	// overflow (8) and a cleared [0] stack. ExecuteGetMethod must do the same.
	// DROP; NEWC; ENDC; PUSHINT16 513; PUSHCONT{NEWC;STREF;ENDC}; REPEAT; POPCTR c4
	code := rawCodeCellFromHex(t, "30C8C981020193C8CCC9E4ED54")
	data := cell.BeginCell().EndCell()

	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("go stack: %v", err)
	}
	execStack := goStack.Copy()
	if err = execStack.PushSmallInt(0); err != nil {
		t.Fatalf("push method id: %v", err)
	}
	cfg, err := crossRunPreparedBlockchainConfig(13)
	if err != nil {
		t.Fatalf("prepare config: %v", err)
	}
	machine := NewTVM()
	goRes, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000_000), execStack, ExecutionConfig{Config: cfg})
	if err != nil {
		t.Fatalf("go get method: %v", err)
	}
	goStackCell, err := stackToCell(goRes.Stack)
	if err != nil {
		t.Fatalf("serialize go stack: %v", err)
	}

	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("ref stack: %v", err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 13))
	refCfg.GasLimit = 100_000_000
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference run: %v", err)
	}

	if goRes.ExitCode != int64(refRes.exitCode) {
		t.Fatalf("exit mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goStackCell.Hash(), refRes.stack.Hash()) {
		t.Fatalf("stack mismatch:\ngo:  %s\nref: %s", goStackCell.Dump(), refRes.stack.Dump())
	}
	if goRes.Committed {
		t.Fatal("too-deep c4 must not commit")
	}
}

func TestTVMCrossEmulatorStackOpEdgeCoverageParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Long-form XCHG invalid argument combos (a==0 or a>=b) and stack-op
	// underflow paths flagged as cross-emulator coverage gaps by the audit.
	cases := []struct {
		name  string
		code  string
		stack []any
	}{
		{name: "xchg-long-a0", code: "301001", stack: []any{int64(1), int64(2)}},
		{name: "xchg-long-eq", code: "301021", stack: []any{int64(1), int64(2), int64(3)}},
		{name: "xchg-long-gt", code: "301032", stack: []any{int64(1), int64(2), int64(3), int64(4)}},
		{name: "xchg-long-valid", code: "301012", stack: []any{int64(1), int64(2), int64(3)}},
		{name: "xchg3-underflow", code: "304123", stack: []any{int64(1), int64(2)}},
		{name: "xcpu-underflow", code: "305123", stack: []any{int64(1)}},
		{name: "xchg2-underflow", code: "305012", stack: []any{int64(1)}},
		{name: "puxc-underflow", code: "305212", stack: nil},
		{name: "sdskipfirst-underflow", code: "30D721", stack: []any{int64(3)}},
		{name: "sdskipfirst-too-long", code: "30D721", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), int64(9)}},
		{name: "sdskipfirst-range", code: "30D721", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), int64(1024)}},
		{name: "sdskipfirst-nan", code: "30D721", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), vm.NaN{}}},
		{name: "sdskipfirst-type", code: "30D721", stack: []any{rawCodeCellFromHex(t, "AB"), int64(3)}},
		{name: "xchg3-long-underflow", code: "30540123", stack: []any{int64(1)}},
		{name: "xc2pu-underflow", code: "30541123", stack: []any{int64(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFlowParityCase(t, tc.name, rawCodeCellFromHex(t, tc.code), tc.stack, 13, 1_000_000)
		})
	}
}

// A truncated opcode is zero-padded to select its opcode-table entry, and gas
// is charged for that entry's full bit length before inv_opcode is thrown —
// marked consensus-critical in the reference opcode table.
func TestTVMCrossEmulatorTruncatedOpcodeGasParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tailCode := func(bits uint64, n uint) *cell.Cell {
		return cell.BeginCell().MustStoreUInt(0x30, 8).MustStoreUInt(bits, n).EndCell()
	}

	cases := []struct {
		name string
		bits uint64
		n    uint
	}{
		{name: "xchg-long-4b", bits: 0b0001, n: 4},
		{name: "xchg-long-5b", bits: 0b00010, n: 5},
		{name: "xchg-long-7b", bits: 0b0001000, n: 7},
		{name: "xchg-short-5b", bits: 0b00011, n: 5},
		{name: "xchg-short-6b", bits: 0b000101, n: 6},
		{name: "pushint8-4b", bits: 0b1000, n: 4},
		{name: "pushint8-7b", bits: 0b1000000, n: 7},
		{name: "pushint-long-7b", bits: 0b1000001, n: 7},
		{name: "pushint-tiny-5b", bits: 0b10001, n: 5},
		{name: "pushslice-ref-6b", bits: 0b100011, n: 6},
		{name: "pushslice-ref-7b", bits: 0b1000110, n: 7},
		{name: "pushrefcont-7b", bits: 0b1000101, n: 7},
		{name: "pushcont-big-7b", bits: 0b1000111, n: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFlowParityCase(t, tc.name, tailCode(tc.bits, tc.n), []any{int64(0)}, 13, 1_000_000)
		})
	}
}

func TestTVMCrossEmulatorLshiftDivModEdgeParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	lshiftDivModC := prependRawMethodDropBuilders(t, mathop.LSHIFTDIVMODC().Serialize())
	lshiftDivModR := prependRawMethodDropBuilders(t, mathop.LSHIFTDIVMODR().Serialize())

	for _, tc := range []struct {
		name  string
		code  *cell.Cell
		stack []any
		vers  []int
	}{
		{name: "lshiftdivmodc-div-zero", code: lshiftDivModC, stack: []any{int64(-7), int64(0), int64(2)}, vers: []int{13}},
		{name: "lshiftdivmodc-nan-legacy", code: lshiftDivModC, stack: []any{vm.NaN{}, int64(5), int64(2)}, vers: []int{10, 13, 14}},
		{name: "lshiftdivmodc-shift-range", code: lshiftDivModC, stack: []any{int64(1), int64(1), int64(400)}, vers: []int{10, 14}},
		{name: "lshiftdivmodr-div-zero", code: lshiftDivModR, stack: []any{int64(-7), int64(0), int64(2)}, vers: []int{13}},
		{name: "lshiftdivmodr-nan-legacy", code: lshiftDivModR, stack: []any{vm.NaN{}, int64(5), int64(2)}, vers: []int{10, 13, 14}},
		{name: "lshiftdivmodr-shift-range", code: lshiftDivModR, stack: []any{int64(1), int64(1), int64(400)}, vers: []int{10, 14}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, version := range tc.vers {
				assertFlowParityCase(t, tc.name, tc.code, tc.stack, version, 1_000_000)
			}
		})
	}
}
