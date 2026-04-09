package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return RUNVM(0) },
		func() vm.OP { return RUNVMX() },
	)
}

func clampRunvmGasLimit(limit, remaining int64) int64 {
	if limit > remaining {
		return remaining
	}
	return limit
}

func runChildVMWithMode(state *vm.State, mode int) error {
	if mode < 0 || mode >= 512 {
		return vmerr.Error(vmerr.CodeRangeCheck, "invalid flags")
	}
	if err := state.ConsumeGas(vm.RunvmGasPrice); err != nil {
		return err
	}

	gasMax := vm.GasInfinite
	if mode&64 != 0 {
		maxVal, err := state.Stack.PopIntRange(0, vm.GasInfinite)
		if err != nil {
			return err
		}
		gasMax = maxVal.Int64()
	}

	gasLimit := vm.GasInfinite
	if mode&8 != 0 {
		limitVal, err := state.Stack.PopIntRange(0, vm.GasInfinite)
		if err != nil {
			return err
		}
		gasLimit = limitVal.Int64()
	}

	if mode&64 == 0 {
		gasMax = gasLimit
	} else if gasMax < gasLimit {
		gasMax = gasLimit
	}

	c7 := tuple.Tuple{}
	if mode&16 != 0 {
		val, err := state.Stack.PopTuple()
		if err != nil {
			return err
		}
		c7 = val
	}

	var data *cell.Cell
	if mode&4 != 0 {
		val, err := state.Stack.PopCell()
		if err != nil {
			return err
		}
		data = val
	}

	retVals := -1
	if mode&256 != 0 {
		val, err := state.Stack.PopIntRange(0, 1<<30)
		if err != nil {
			return err
		}
		retVals = int(val.Int64())
	}

	code, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	maxStackSize := state.Stack.Len() - 1
	if maxStackSize < 0 {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	stackSizeVal, err := state.Stack.PopIntRange(0, int64(maxStackSize))
	if err != nil {
		return err
	}
	stackSize := int(stackSizeVal.Int64())

	childStack := vm.NewStack()
	if stackSize > 0 {
		if err = childStack.MoveFrom(state.Stack, stackSize); err != nil {
			return err
		}
	}
	if err = state.ConsumeStackGas(childStack); err != nil {
		return err
	}

	gasMax = clampRunvmGasLimit(gasMax, state.Gas.Remaining)
	gasLimit = clampRunvmGasLimit(gasLimit, state.Gas.Remaining)
	if gasMax < gasLimit {
		gasMax = gasLimit
	}

	childGas := vm.Gas{}
	childGas.SetLimits(gasMax, gasLimit)

	return state.RunChildVM(vm.ChildVMConfig{
		Code:          code,
		Stack:         childStack,
		Data:          data,
		C7:            c7,
		Gas:           childGas,
		SameC3:        mode&1 != 0,
		PushZero:      mode&1 != 0 && mode&2 != 0,
		ReturnData:    mode&4 != 0,
		ReturnActions: mode&32 != 0,
		ReturnGas:     mode&8 != 0,
		IsolateGas:    mode&128 != 0,
		ReturnValues:  retVals,
	})
}

func RUNVM(mode int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("RUNVM %d", mode)
		},
		BitPrefix:     helpers.SlicePrefix(12, []byte{0xDB, 0x40}),
		FixedSizeBits: 12,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(mode), 12)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(12)
			if err != nil {
				return err
			}
			mode = int(val)
			return nil
		},
		Action: func(state *vm.State) error {
			return runChildVMWithMode(state, mode)
		},
	}
}

func RUNVMX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "RUNVMX",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x50),
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopIntRange(0, 4095)
			if err != nil {
				return err
			}
			return runChildVMWithMode(state, int(val.Int64()))
		},
	}
}
