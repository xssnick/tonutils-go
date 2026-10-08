//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTVMCrossEmulatorStackValueCopyOnWrite(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name         string
		instructions []*cell.Builder
		stack        []any
	}{
		{
			name: "tuple_setindex_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				tupleop.SETINDEX(0).Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(1), big.NewInt(2))},
		},
		{
			name: "tuple_setindexq_extend_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				tupleop.SETINDEXQ(3).Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(1))},
		},
		{
			name: "tuple_tpush_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				tupleop.TPUSH().Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(1))},
		},
		{
			name: "tuple_tpop_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				tupleop.TPOP().Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(1), big.NewInt(2))},
		},
		{
			name: "nested_tuple_setindex_after_dup_and_index",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				tupleop.INDEX(0).Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				tupleop.SETINDEX(0).Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(
				tuplepkg.NewTupleValue(big.NewInt(1), big.NewInt(2)),
			)},
		},
		{
			name: "slice_cursor_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				cellsliceop.LDU(4).Serialize(),
			},
			stack: []any{cell.BeginCell().MustStoreUInt(0xab, 8).EndCell().MustBeginParse()},
		},
		{
			name: "tuple_slice_cursor_after_dup_and_index",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				tupleop.INDEX(0).Serialize(),
				cellsliceop.LDU(4).Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(
				cell.BeginCell().MustStoreUInt(0xab, 8).EndCell().MustBeginParse(),
			)},
		},
		{
			name: "builder_after_dup",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				stackop.SWAP().Serialize(),
				cellsliceop.STU(4).Serialize(),
			},
			stack: []any{cell.BeginCell()},
		},
		{
			name: "tuple_builder_after_dup_and_index",
			instructions: []*cell.Builder{
				stackop.DUP().Serialize(),
				tupleop.INDEX(0).Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
				stackop.SWAP().Serialize(),
				cellsliceop.STU(4).Serialize(),
			},
			stack: []any{tuplepkg.NewTupleValue(cell.BeginCell())},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			initial, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build initial stack: %v", err)
			}
			code := buildStackProgram(t, tt.instructions)
			runStackOpParityProgram(t, code, initial, 0)
		})
	}
}
