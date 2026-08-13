//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorSetGlobTupleGasThreshold(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "fixed",
			code:  codeFromBuilders(t, funcsop.SETGLOB(1).Serialize()),
			stack: []any{int64(7)},
		},
		{
			name:  "variable",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize()),
			stack: []any{int64(7), int64(1)},
		},
	}
	gasLimits := []int64{43, 44, 45, 46, 50, 51}

	for _, tt := range tests {
		for _, gasLimit := range gasLimits {
			t.Run(fmt.Sprintf("%s/gas_%d", tt.name, gasLimit), func(t *testing.T) {
				code := prependRawMethodDrop(tt.code)
				c7 := tuple.NewTupleValue(tuple.NewTupleValue())
				goStack, err := buildCrossStack(tt.stack...)
				if err != nil {
					t.Fatalf("build go stack: %v", err)
				}
				refStack, err := buildCrossStack(tt.stack...)
				if err != nil {
					t.Fatalf("build reference stack: %v", err)
				}

				goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), c7, goStack, gasLimit)
				if err != nil {
					t.Fatalf("go tvm execution failed: %v", err)
				}
				refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), c7, refStack, gasLimit)
				if err != nil {
					t.Fatalf("reference tvm execution failed: %v", err)
				}

				wantExit := int32(0)
				if gasLimit < 51 {
					wantExit = int32(^vmerr.CodeOutOfGas)
				}
				if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
					t.Fatalf("exit mismatch: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
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
			})
		}
	}
}
