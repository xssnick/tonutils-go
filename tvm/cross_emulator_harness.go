//go:build cgo && tvm_cross_emulator

package tvm

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type crossRunResult struct {
	exitCode int32
	gasUsed  int64
	stack    *cell.Cell
}

func buildCrossStack(values ...any) (*vm.Stack, error) {
	stack := vm.NewStack()
	for _, value := range values {
		switch v := value.(type) {
		case int:
			if err := stack.PushInt(big.NewInt(int64(v))); err != nil {
				return nil, err
			}
		case int64:
			if err := stack.PushInt(big.NewInt(v)); err != nil {
				return nil, err
			}
		case *big.Int:
			if err := stack.PushInt(v); err != nil {
				return nil, err
			}
		default:
			if err := stack.PushAny(value); err != nil {
				return nil, err
			}
		}
	}
	return stack, nil
}

func runGoCrossCode(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack) (*crossRunResult, error) {
	return runGoCrossCodeWithLibs(code, data, c7, nil, stack)
}

func runGoCrossCodeWithGas(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack, gasLimit int64) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionGasAndLibs(code, data, c7, nil, stack, vm.DefaultGlobalVersion, gasLimit)
}

func runGoCrossCodeWithVersion(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack, globalVersion int) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionAndLibs(code, data, c7, nil, stack, globalVersion)
}

func runGoCrossCodeWithLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionAndLibs(code, data, c7, libs, stack, vm.DefaultGlobalVersion)
}

func runGoCrossCodeWithVersionAndLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack, globalVersion int) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionGasAndLibs(code, data, c7, libs, stack, globalVersion, crossTestMaxGas)
}

func runGoCrossCodeWithVersionGasAndLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack, globalVersion int, gasLimit int64) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, err
	}

	machine := NewTVM()
	machine.globalVersion = globalVersion
	res, err := machine.ExecuteDetailedWithLibraries(code, data, c7, vm.GasWithLimit(gasLimit), execStack, libs...)
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	exitCode := int32(0)
	if err != nil {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) {
			return nil, err
		}
		exitCode = int32(vmErr.Code)
	} else {
		exitCode = int32(res.ExitCode)
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: exitCode,
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func tupleToStackCell(v tuple.Tuple) (*cell.Cell, error) {
	stack := tlb.NewStack()
	stack.Push(tupleToAny(v))
	return stack.ToCell()
}

func tupleToAny(v tuple.Tuple) []any {
	res := make([]any, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		val, err := v.Index(i)
		if err != nil {
			panic(err)
		}

		if nested, ok := val.(tuple.Tuple); ok {
			res = append(res, tupleToAny(nested))
			continue
		}

		res = append(res, normalizeTLBStackValue(val))
	}
	return res
}

func stackToCell(stack *vm.Stack) (*cell.Cell, error) {
	cp := stack.Copy()
	tlbStack := tlb.NewStack()
	for range cp.Len() {
		val, err := cp.PopAny()
		if err != nil {
			return nil, err
		}
		tlbStack.Push(normalizeTLBStackValue(val))
	}
	return tlbStack.ToCell()
}

func normalizeTLBStackValue(val any) any {
	switch v := val.(type) {
	case tuple.Tuple:
		return tupleToAny(v)
	case *cell.Cell:
		if v == nil {
			return nil
		}
		return v
	case *cell.Slice:
		if v == nil {
			return nil
		}
		return v
	case *cell.Builder:
		if v == nil {
			return nil
		}
		return v
	default:
		return val
	}
}

func prependRawMethodDrop(code *cell.Cell) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0x30, 8).
		MustStoreBuilder(code.ToBuilder()).
		EndCell()
}

func normalizeStackCell(cl *cell.Cell) (*cell.Cell, error) {
	var stack tlb.Stack
	if err := stack.LoadFromCell(cl.BeginParse()); err != nil {
		return nil, err
	}
	return stack.ToCell()
}
