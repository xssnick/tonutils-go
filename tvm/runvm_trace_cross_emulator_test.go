//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func runVMContinuationTraceCode() *cell.Cell {
	leaf := cell.BeginCell().MustStoreUInt(0x77, 8).EndCell() // PUSHINT 7
	body := cell.BeginCell().
		MustStoreUInt(0xDB3C, 16). // CALLREF
		MustStoreRef(leaf).
		EndCell()
	child := cell.BeginCell().MustStoreUInt(0xD8, 8).EndCell() // EXECUTE

	return prependRawMethodDrop(cell.BeginCell().
		MustStoreUInt(0x89, 8).MustStoreRef(body).  // PUSHREFSLICE
		MustStoreUInt(0xED1E71, 24).                // BLESS; PUSHINT 1
		MustStoreUInt(0x89, 8).MustStoreRef(child). // PUSHREFSLICE
		MustStoreUInt(0xDB4000, 24).                // RUNVM 0
		EndCell())
}

func runVMSharedTupleTraceCode(depth int, output bool) *cell.Cell {
	dag := cell.BeginCell().
		MustStoreUInt(0x89, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).
		MustStoreUInt(0x6F01, 16) // PUSHREFSLICE; TUPLE 1
	for range depth {
		dag.MustStoreUInt(0x206F02, 24) // DUP; TUPLE 2
	}

	if !output {
		child := cell.BeginCell().MustStoreUInt(0x30, 8).EndCell() // DROP
		return prependRawMethodDrop(dag.
			MustStoreUInt(0x7189, 16).MustStoreRef(child). // PUSHINT 1; PUSHREFSLICE
			MustStoreUInt(0xDB4000, 24).                   // RUNVM 0
			EndCell())
	}

	return prependRawMethodDrop(cell.BeginCell().
		MustStoreUInt(0x7089, 16).MustStoreRef(dag.EndCell()). // PUSHINT 0; PUSHREFSLICE
		MustStoreUInt(0xDB4000, 24).                           // RUNVM 0
		MustStoreUInt(0x3030, 16).                             // DROP exit; DROP tuple
		EndCell())
}

func TestTVMCrossEmulatorRunVMContinuationTraceParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := runVMContinuationTraceCode()
	for _, gasLimit := range []int64{600, 1_000_000} {
		t.Run(fmt.Sprintf("gas_%d", gasLimit), func(t *testing.T) {
			assertFlowParityCase(t, "continuation-input", code, nil, 13, gasLimit)
		})
	}
}

func TestTVMCrossEmulatorRunVMSharedTupleTraceParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, output := range []bool{false, true} {
		for _, depth := range []int{0, 8, 16} {
			name := fmt.Sprintf("input_%t/depth_%d", !output, depth)
			t.Run(name, func(t *testing.T) {
				assertFlowParityCase(t, name, runVMSharedTupleTraceCode(depth, output), nil, 13, 1_000_000)
			})
		}
	}
}
