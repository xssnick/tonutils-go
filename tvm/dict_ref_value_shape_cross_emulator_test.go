//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorDictRefValueShapeParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	key := cell.BeginCell().MustStoreUInt(0x12, 8).EndCell()
	keySlice := key.MustBeginParse()
	newRef := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	shapes := []struct {
		name  string
		value *cell.Builder
	}{
		{
			name:  "payload-bit",
			value: cell.BeginCell().MustStoreBoolBit(true),
		},
		{
			name: "two-refs",
			value: cell.BeginCell().
				MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
				MustStoreRef(cell.BeginCell().MustStoreUInt(0, 1).EndCell()),
		},
	}

	tests := []struct {
		name   string
		opcode string
		stack  func(*cell.Cell) []any
	}{
		{
			name:   "dictgetref",
			opcode: "F40B",
			stack: func(root *cell.Cell) []any {
				return []any{keySlice.Copy(), root, int64(8)}
			},
		},
		{
			name:   "dictsetgetref",
			opcode: "F41B",
			stack: func(root *cell.Cell) []any {
				return []any{newRef, keySlice.Copy(), root, int64(8)}
			},
		},
		{
			name:   "dictdelgetref",
			opcode: "F463",
			stack: func(root *cell.Cell) []any {
				return []any{keySlice.Copy(), root, int64(8)}
			},
		},
		{
			name:   "dictgetoptref",
			opcode: "F469",
			stack: func(root *cell.Cell) []any {
				return []any{keySlice.Copy(), root, int64(8)}
			},
		},
		{
			name:   "dictsetgetoptref",
			opcode: "F46D",
			stack: func(root *cell.Cell) []any {
				return []any{newRef, keySlice.Copy(), root, int64(8)}
			},
		},
		{
			name:   "dictminref",
			opcode: "F483",
			stack: func(root *cell.Cell) []any {
				return []any{root, int64(8)}
			},
		},
		{
			name:   "dictremminref",
			opcode: "F493",
			stack: func(root *cell.Cell) []any {
				return []any{root, int64(8)}
			},
		},
		{
			name:   "dictremmaxref",
			opcode: "F49B",
			stack: func(root *cell.Cell) []any {
				return []any{root, int64(8)}
			},
		},
	}

	for _, shape := range shapes {
		dict := cell.NewDict(8)
		if _, err := dict.SetBuilderWithMode(key, shape.value, cell.DictSetModeSet); err != nil {
			t.Fatalf("build %s dictionary: %v", shape.name, err)
		}
		root := dict.AsCell()

		for _, tt := range tests {
			t.Run(shape.name+"/"+tt.name, func(t *testing.T) {
				runCellParityCases(t, []cellParityCase{{
					name:  tt.name,
					code:  rawCodeCellFromHex(t, tt.opcode),
					stack: tt.stack(root),
					exit:  vmerr.CodeDict,
				}})
			})
		}

		// Integer-key variants share the same extraction path, but pin one
		// representative so future family refactors cannot skip it silently.
		t.Run(shape.name+"/dictugetref", func(t *testing.T) {
			runCellParityCases(t, []cellParityCase{{
				name:  "dictugetref",
				code:  rawCodeCellFromHex(t, "F40F"),
				stack: []any{big.NewInt(0x12), root, int64(8)},
				exit:  vmerr.CodeDict,
			}})
		})
	}
}
