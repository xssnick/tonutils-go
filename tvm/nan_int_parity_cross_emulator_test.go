//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorRist255MulNaNParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	point := testRistrettoMulBaseInt(t, 1)
	tests := []struct {
		name     string
		op       *cell.Builder
		stack    []any
		exit     int32
		versions []int
	}{
		{
			name:     "RIST255_MUL NaN point",
			op:       funcsop.RIST255_MUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(1)},
			exit:     int32(vmerr.CodeRangeCheck),
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_MUL NaN scalar",
			op:       funcsop.RIST255_MUL().Serialize(),
			stack:    []any{point, vm.NaN{}},
			exit:     int32(vmerr.CodeRangeCheck),
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_QMUL NaN point",
			op:       funcsop.RIST255_QMUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(1)},
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_QMUL NaN scalar",
			op:       funcsop.RIST255_QMUL().Serialize(),
			stack:    []any{point, vm.NaN{}},
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_MULBASE NaN scalar",
			op:       funcsop.RIST255_MULBASE().Serialize(),
			stack:    []any{vm.NaN{}},
			exit:     int32(vmerr.CodeRangeCheck),
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_QMULBASE NaN scalar",
			op:       funcsop.RIST255_QMULBASE().Serialize(),
			stack:    []any{vm.NaN{}},
			versions: []int{13, 14},
		},
		{
			name:     "RIST255_MUL legacy zero scalar skips NaN point",
			op:       funcsop.RIST255_MUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(0)},
			versions: []int{13},
		},
		{
			name:     "RIST255_QMUL legacy zero scalar skips NaN point",
			op:       funcsop.RIST255_QMUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(0)},
			versions: []int{13},
		},
		{
			name:     "RIST255_MUL v14 zero scalar validates NaN point",
			op:       funcsop.RIST255_MUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(0)},
			exit:     int32(vmerr.CodeRangeCheck),
			versions: []int{14},
		},
		{
			name:     "RIST255_QMUL v14 zero scalar validates NaN point",
			op:       funcsop.RIST255_QMUL().Serialize(),
			stack:    []any{vm.NaN{}, int64(0)},
			versions: []int{14},
		},
	}

	for _, tt := range tests {
		for _, version := range tt.versions {
			t.Run(fmt.Sprintf("%s v%d", tt.name, version), func(t *testing.T) {
				runTonOpsEdgeVersionedParityCase(
					t,
					codeFromBuilders(t, tt.op),
					tt.stack,
					tuple.Tuple{},
					version,
					tt.exit,
					0,
				)
			})
		}
	}
}

func TestTVMCrossEmulatorBLSMultiexpNaNScalar(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name  string
		op    *cell.Builder
		stack []any
		gas   int64
	}{
		{
			name:  "BLS_G1_MULTIEXP",
			op:    funcsop.BLS_G1_MULTIEXP().Serialize(),
			stack: []any{testSliceFromBytes(testBLSG1BytesForScalar(2)), vm.NaN{}, int64(1)},
			gas:   14_312,
		},
		{
			name:  "BLS_G2_MULTIEXP",
			op:    funcsop.BLS_G2_MULTIEXP().Serialize(),
			stack: []any{testSliceFromBytes(testBLSG2BytesForScalar(2)), vm.NaN{}, int64(1)},
			gas:   37_480,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := codeFromBuilders(t, tt.op)
			runTonOpsEdgeVersionedParityCase(
				t,
				code,
				tt.stack,
				tuple.Tuple{},
				14,
				int32(vmerr.CodeIntOverflow),
				0,
			)

			stack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build gas-check stack: %v", err)
			}
			res, err := runGoCrossCodeWithVersion(
				prependRawMethodDrop(code),
				testEmptyCell(),
				tuple.Tuple{},
				stack,
				14,
			)
			if err != nil {
				t.Fatalf("Go gas-check execution failed: %v", err)
			}
			if res.gasUsed != tt.gas {
				t.Fatalf("gas used = %d, want %d", res.gasUsed, tt.gas)
			}
		})
	}
}

func TestTVMCrossEmulatorFeeAndPRNGNaNOrder(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	wrongSeedParams := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &wrongSeedParams, 6, cell.BeginCell().EndCell())
	wrongSeedC7 := tuple.NewTupleValue(wrongSeedParams)

	nanSeedParams := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &nanSeedParams, 6, vm.NaN{})
	nanSeedC7 := tuple.NewTupleValue(nanSeedParams)

	tests := []struct {
		name  string
		op    *cell.Builder
		stack []any
		c7    tuple.Tuple
		exit  int32
	}{
		{name: "GETGASFEE NaN gas", op: funcsop.GETGASFEE().Serialize(), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETGASFEE NaN chain flag precedes gas type", op: funcsop.GETGASFEE().Serialize(), stack: []any{cell.BeginCell().EndCell(), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "GETSTORAGEFEE NaN cells", op: funcsop.GETSTORAGEFEE().Serialize(), stack: []any{vm.NaN{}, int64(2), int64(3), int64(0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETFORWARDFEE NaN cells", op: funcsop.GETFORWARDFEE().Serialize(), stack: []any{vm.NaN{}, int64(2), int64(0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETORIGINALFWDFEE NaN fee", op: funcsop.GETORIGINALFWDFEE().Serialize(), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "GETGASFEESIMPLE NaN gas", op: funcsop.GETGASFEESIMPLE().Serialize(), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETFORWARDFEESIMPLE NaN cells", op: funcsop.GETFORWARDFEESIMPLE().Serialize(), stack: []any{vm.NaN{}, int64(2), int64(0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETEXTRABALANCE NaN id", op: funcsop.GETEXTRABALANCE().Serialize(), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RAND NaN operand precedes seed type", op: funcsop.RAND().Serialize(), stack: []any{vm.NaN{}}, c7: wrongSeedC7, exit: int32(vmerr.CodeIntOverflow)},
		{name: "ADDRAND NaN operand precedes NaN seed", op: funcsop.ADDRAND().Serialize(), stack: []any{vm.NaN{}}, c7: nanSeedC7, exit: int32(vmerr.CodeIntOverflow)},
		{name: "ADDRAND negative operand precedes seed type", op: funcsop.ADDRAND().Serialize(), stack: []any{int64(-1)}, c7: wrongSeedC7, exit: int32(vmerr.CodeRangeCheck)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(
				t,
				prependRawMethodDrop(codeFromBuilders(t, tt.op)),
				tt.stack,
				tt.c7,
				tt.exit,
				0,
			)
		})
	}
}
