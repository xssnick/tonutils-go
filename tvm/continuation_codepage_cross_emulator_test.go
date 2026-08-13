//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorOrdinaryContinuationAbsentCodepage(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	newContinuation := func() *vm.OrdinaryContinuation {
		return &vm.OrdinaryContinuation{
			Data: vm.ControlData{
				NumArgs: vm.ControlDataAllArgs,
				CP:      vm.CP,
			},
			Code: rawCodeCellFromHex(t, "77DB30").MustBeginParse(), // PUSHINT 7; RET
		}
	}
	goStack, err := buildCrossStack(newContinuation())
	if err != nil {
		t.Fatalf("build go stack: %v", err)
	}
	refStack, err := buildCrossStack(newContinuation())
	if err != nil {
		t.Fatalf("build reference stack: %v", err)
	}

	code := rawCodeCellFromHex(t, "30D8") // DROP; EXECUTE
	data := cell.BeginCell().EndCell()
	goRes, err := runGoCrossCodeWithVersion(code, data, tuple.Tuple{}, goStack, referenceRawRunGlobalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(referenceRawRunGlobalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	wantExit := int32(vmerr.CodeInvalidOpcode)
	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
