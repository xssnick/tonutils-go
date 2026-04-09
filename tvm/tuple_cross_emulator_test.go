//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTupleOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	twoTuple := *tuplepkg.NewTuple(big.NewInt(7), big.NewInt(8))

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
	}

	tests := []testCase{
		{
			name:  "indexq_out_of_range_nil",
			code:  codeFromBuilders(t, tupleop.INDEXQ(5).Serialize()),
			stack: []any{twoTuple},
			exit:  0,
		},
		{
			name:  "indexvarq_out_of_range_nil",
			code:  codeFromBuilders(t, tupleop.INDEXVARQ().Serialize()),
			stack: []any{twoTuple, int64(5)},
			exit:  0,
		},
		{
			name:  "setindexvarq_nil_tuple_nil_value",
			code:  codeFromBuilders(t, tupleop.SETINDEXVARQ().Serialize()),
			stack: []any{nil, nil, int64(1)},
			exit:  0,
		},
		{
			name:  "qtlen_non_tuple",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{int64(10)},
			exit:  0,
		},
		{
			name:  "istuple_non_tuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{int64(1)},
			exit:  0,
		},
		{
			name:  "last_non_tuple_typecheck",
			code:  codeFromBuilders(t, tupleop.LAST().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "untuple_exact_arity_mismatch",
			code:  codeFromBuilders(t, tupleop.UNTUPLE(3).Serialize()),
			stack: []any{twoTuple},
			exit:  vmerr.CodeTypeCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)

			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, testEmptyCell(), tuplepkg.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuplepkg.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}
