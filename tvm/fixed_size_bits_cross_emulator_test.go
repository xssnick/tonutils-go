//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTVMCrossEmulatorFixedSizeBitsOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	leafTuple := *tuplepkg.NewTuple(big.NewInt(2), big.NewInt(3))
	midTuple := *tuplepkg.NewTuple(big.NewInt(1), leafTuple)
	nestedTuple := *tuplepkg.NewTuple(midTuple)

	type testCase struct {
		name         string
		code         *cell.Cell
		stack        []any
		exit         int32
		noStackCheck bool
	}

	tests := []testCase{
		{
			name: "tuple_make",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(11)).Serialize(),
				stackop.PUSHINT(big.NewInt(22)).Serialize(),
				tupleop.TUPLE(2).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "index3_nested",
			code:  codeFromBuilders(t, tupleop.INDEX3(0, 1, 0).Serialize()),
			stack: []any{nestedTuple},
			exit:  0,
		},
		{
			name:  "indexq_out_of_range",
			code:  codeFromBuilders(t, tupleop.INDEXQ(2).Serialize()),
			stack: []any{*tuplepkg.NewTuple(big.NewInt(11))},
			exit:  0,
		},
		{
			name:  "qtlen_non_tuple",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
		},
		{
			name:  "istuple_non_tuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
		},
		{
			name: "jmpxargs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())).Serialize(),
				execop.JMPXARGS(0).Serialize(),
			),
			exit: 0,
		},
		{
			name: "throwany",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2F0, 16),
			),
			exit:         44,
			noStackCheck: true,
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

			goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tuplepkg.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tuplepkg.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			if tt.noStackCheck {
				return
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
