//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTVMCrossEmulatorLDREFRTOSFailedChildLoadState(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Exception dispatch hides the instruction-local remainder from the
	// handler; the cellslice unit test locks that ordering, while this case
	// checks the externally visible exit, gas, and final stack against C++.
	library, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("build library cell: %v", err)
	}
	parent := cell.BeginCell().
		MustStoreUInt(0xA, 4).
		MustStoreRef(library).
		EndCell()
	body := codeFromBuilders(t, cellsliceop.LDREFRTOS().Serialize())
	handler := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(0xCAFE)).Serialize())
	code := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHCONT(body).Serialize(),
		stackop.PUSHCONT(handler).Serialize(),
		execop.TRY().Serialize(),
	))

	goStack, err := buildCrossStack(parent.MustBeginParse())
	if err != nil {
		t.Fatalf("build Go stack: %v", err)
	}
	refStack, err := buildCrossStack(parent.MustBeginParse())
	if err != nil {
		t.Fatalf("build reference stack: %v", err)
	}
	goRes, err := runGoCrossCode(code, testEmptyCell(), tuple.Tuple{}, goStack)
	if err != nil {
		t.Fatalf("Go TVM execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuple.Tuple{}, refStack)
	if err != nil {
		t.Fatalf("reference TVM execution failed: %v", err)
	}
	if goRes.exitCode != 0 || refRes.exitCode != 0 {
		t.Fatalf("exit code = Go %d, reference %d, want 0", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas = Go %d, reference %d", goRes.gasUsed, refRes.gasUsed)
	}

	goResult, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("normalize Go stack: %v", err)
	}
	refResult, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("normalize reference stack: %v", err)
	}
	if !bytes.Equal(goResult.Hash(), refResult.Hash()) {
		t.Fatalf("stack mismatch:\nGo=%s\nreference=%s", goResult.Dump(), refResult.Dump())
	}
}
