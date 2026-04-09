//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTVMCrossEmulatorThrowOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name         string
		code         *cell.Cell
		stack        []any
		exit         int32
		noStackCheck bool
	}

	tests := []testCase{
		{
			name: "throwif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF261, 16)),
			stack: []any{
				int64(-1),
			},
			exit:         0x21,
			noStackCheck: true,
		},
		{
			name: "throwif_false_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF25B, 16)),
			stack: []any{
				int64(777),
				int64(0),
			},
			exit: 0,
		},
		{
			name: "throwargif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2D955, 24)),
			stack: []any{
				int64(321),
				int64(-1),
			},
			exit:         0x155,
			noStackCheck: true,
		},
		{
			name: "throwargifnot_true_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2E888, 24)),
			stack: []any{
				int64(999),
				int64(321),
				int64(-1),
			},
			exit: 0,
		},
		{
			name: "throwanyif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				int64(0x222),
				int64(-1),
			},
			exit:         0x222,
			noStackCheck: true,
		},
		{
			name: "throwarganyifnot_false",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F5, 16)),
			stack: []any{
				int64(0x77),
				int64(0x222),
				int64(0),
			},
			exit:         0x222,
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

			goRes, err := runGoCrossCode(code, testEmptyCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuple.Tuple{}, refStack)
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
