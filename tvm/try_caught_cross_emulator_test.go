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

func TestTVMCrossEmulatorCaughtTryEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	handler := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(0xCAFE)).Serialize())
	tryCode := func(body *cell.Cell) *cell.Cell {
		return prependRawMethodDrop(codeFromBuilders(t,
			stackop.PUSHCONT(body).Serialize(),
			stackop.PUSHCONT(handler).Serialize(),
			execop.TRY().Serialize(),
		))
	}

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
	}

	tests := []testCase{
		{
			name: "throw_no_arg",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF225, 16),
			)),
			stack: []any{int64(777)},
		},
		{
			name: "throwarg",
			code: tryCode(codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(321)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2C955, 24),
			)),
			stack: []any{int64(888)},
		},
		{
			name: "throwif_true",
			code: tryCode(codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF261, 16),
			)),
			stack: []any{int64(999)},
		},
		{
			name: "ldu_underflow",
			code: tryCode(codeFromBuilders(t,
				cellsliceop.LDU(8).Serialize(),
			)),
			stack: []any{cell.BeginCell().EndCell().BeginParse(), int64(111)},
		},
		{
			name: "ldref_underflow",
			code: tryCode(codeFromBuilders(t,
				cellsliceop.LDREF().Serialize(),
			)),
			stack: []any{cell.BeginCell().MustStoreUInt(0xA, 4).EndCell().BeginParse(), int64(222)},
		},
		{
			name: "dictset_short_key_underflow",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF412, 16),
			)),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(),
				cell.BeginCell().MustStoreUInt(0x1, 4).EndCell().BeginParse(),
				nil,
				int64(8),
			},
		},
		{
			name: "dictsetgetoptref_short_key_underflow",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF46D, 16),
			)),
			stack: []any{
				cell.BeginCell().EndCell(),
				cell.BeginCell().MustStoreUInt(0x1, 4).EndCell().BeginParse(),
				nil,
				int64(8),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != 0 || refRes.exitCode != 0 {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=0", goRes.exitCode, refRes.exitCode)
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
