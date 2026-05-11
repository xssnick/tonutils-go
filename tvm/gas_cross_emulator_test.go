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
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorGasBeyondOutOfGas(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	intCode := func(v int64) *cell.Builder {
		return stackop.PUSHINT(big.NewInt(v)).Serialize()
	}
	body := func(builders ...*cell.Builder) *cell.Cell {
		return codeFromBuilders(t, builders...)
	}

	tupleHeavyStack := make([]any, 0, 15)
	for i := 0; i < cap(tupleHeavyStack); i++ {
		tupleHeavyStack = append(tupleHeavyStack, int64(i+1))
	}

	throwArgBody := body(
		intCode(321),
		cell.BeginCell().MustStoreUInt(0xF2C955, 24),
	)
	exceptionTupleHandler := body(
		tupleop.TUPLE(15).Serialize(),
		tupleop.EXPLODE(15).Serialize(),
	)
	stackGrowthBody := body(
		intCode(1), intCode(2), intCode(3), intCode(4),
		intCode(5), intCode(6), intCode(7), intCode(8),
		intCode(9), intCode(10), intCode(11), intCode(12),
		intCode(13), intCode(14), intCode(15), intCode(16),
		intCode(17), intCode(18), intCode(19), intCode(20),
	)

	type testCase struct {
		name     string
		code     *cell.Cell
		stack    []any
		gasLimit int64
	}

	tests := []testCase{
		{
			name: "tuple_build_overruns_limit",
			code: prependRawMethodDrop(body(
				tupleop.TUPLE(15).Serialize(),
				tupleop.EXPLODE(15).Serialize(),
			)),
			stack:    tupleHeavyStack,
			gasLimit: 45,
		},
		{
			name:     "stack_growth_overruns_limit",
			code:     prependRawMethodDrop(stackGrowthBody),
			gasLimit: 115,
		},
		{
			name: "exception_handler_tuple_gas_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(throwArgBody).Serialize(),
				stackop.PUSHCONT(exceptionTupleHandler).Serialize(),
				execop.TRY().Serialize(),
			)),
			stack:    tupleHeavyStack,
			gasLimit: 155,
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

			goRes, err := runGoCrossCodeWithGas(tt.code, testEmptyCell(), tuple.Tuple{}, goStack, tt.gasLimit)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCodeWithGas(tt.code, testEmptyCell(), tuple.Tuple{}, refStack, tt.gasLimit)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != int32(^vmerr.CodeOutOfGas) || refRes.exitCode != int32(^vmerr.CodeOutOfGas) {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, ^vmerr.CodeOutOfGas)
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

func TestTVMCrossEmulatorGasRepeatedCellLoads(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	loadAndDrop := func(c *cell.Cell) []*cell.Builder {
		return []*cell.Builder{
			stackop.PUSHREF(c).Serialize(),
			cellsliceop.CTOS().Serialize(),
			stackop.DROP().Serialize(),
		}
	}
	codeFromParts := func(parts ...[]*cell.Builder) *cell.Cell {
		var builders []*cell.Builder
		for _, part := range parts {
			builders = append(builders, part...)
		}
		return prependRawMethodDrop(codeFromBuilders(t, builders...))
	}

	sharedCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	firstCell := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	secondCell := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()
	thirdCell := cell.BeginCell().MustStoreUInt(0x33, 8).EndCell()

	tests := []struct {
		name string
		code *cell.Cell
	}{
		{
			name: "same_cell_loaded_three_times",
			code: codeFromParts(loadAndDrop(sharedCell), loadAndDrop(sharedCell), loadAndDrop(sharedCell)),
		},
		{
			name: "distinct_cells_loaded_once_each",
			code: codeFromParts(loadAndDrop(firstCell), loadAndDrop(secondCell), loadAndDrop(thirdCell)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
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

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
			}
		})
	}
}
