//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type crossRunResult struct {
	exitCode int32
	gasUsed  int64
	stack    *cell.Cell
}

const referenceRawRunGlobalVersion = 13

func buildCrossStack(values ...any) (*vm.Stack, error) {
	stack := vm.NewStack()
	for _, value := range values {
		if v, ok := value.(int64); ok {
			value = big.NewInt(v)
		}
		if err := stack.PushHostValue(value); err != nil {
			return nil, err
		}
	}
	return stack, nil
}

func runGoCrossCode(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack) (*crossRunResult, error) {
	return runGoCrossCodeWithLibs(code, data, c7, nil, stack)
}

func runGoCrossCodeWithGas(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack, gasLimit int64) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionGasAndLibs(code, data, c7, nil, stack, referenceRawRunGlobalVersion, gasLimit)
}

func runGoCrossCodeWithVersion(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack, globalVersion int) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionAndLibs(code, data, c7, nil, stack, globalVersion)
}

func runGoCrossCodeWithLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionAndLibs(code, data, c7, libs, stack, referenceRawRunGlobalVersion)
}

func runGoCrossCodeWithVersionAndLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack, globalVersion int) (*crossRunResult, error) {
	return runGoCrossCodeWithVersionGasAndLibs(code, data, c7, libs, stack, globalVersion, referenceDefaultMaxGas)
}

func runGoCrossCodeWithVersionGasAndLibs(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vm.Stack, globalVersion int, gasLimit int64) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(globalVersion)
	if err != nil {
		return nil, err
	}
	res, err := machine.Execute(code, data, c7, vm.GasWithLimit(gasLimit), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})
	if err != nil {
		return nil, err
	}
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func crossRunPreparedBlockchainConfig(globalVersion int) (*PreparedBlockchainConfig, error) {
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(globalVersion)})
	if err != nil {
		return nil, err
	}

	dict := cell.NewDict(32)
	value := cell.BeginCell().MustStoreRef(versionCell).EndCell()
	if err = dict.SetIntKey(new(big.Int).SetUint64(uint64(tlb.ConfigParamGlobalVersion)), value); err != nil {
		return nil, err
	}
	return PrepareBlockchainConfig(dict.AsCell())
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
	if err := tlb.Parse(&stack, cl); err != nil {
		return nil, err
	}

	canonical := tlb.NewStack()
	values := make([]any, 0, stack.Depth())
	for stack.Depth() > 0 {
		val, err := stack.Pop()
		if err != nil {
			return nil, err
		}
		val, err = canonicalizeCrossStackValue(val)
		if err != nil {
			return nil, err
		}
		values = append(values, val)
	}
	for i := len(values) - 1; i >= 0; i-- {
		canonical.Push(values[i])
	}
	return canonical.ToCell()
}

func canonicalizeCrossStackValue(val any) (any, error) {
	switch v := val.(type) {
	case *cell.Slice:
		if v == nil {
			return nil, nil
		}
		c, err := v.ToCell()
		if err != nil {
			return nil, err
		}
		return c.BeginParse()
	case []any:
		cp := make([]any, len(v))
		for i := range v {
			var err error
			cp[i], err = canonicalizeCrossStackValue(v[i])
			if err != nil {
				return nil, err
			}
		}
		return cp, nil
	default:
		return val, nil
	}
}
