package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const invalidContinuationArgs = 0x40000000

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SETCONTARGS(0, -1) },
		func() vm.OP { return RETURNARGS(0) },
		func() vm.OP { return RETURNVARARGS() },
		func() vm.OP { return SETCONTVARARGS() },
		func() vm.OP { return SETNUMVARARGS() },
		func() vm.OP { return BLESS() },
		func() vm.OP { return BLESSVARARGS() },
		func() vm.OP { return BLESSARGS(0, -1) },
	)
}

func closureStackOverflow() error {
	return vmerr.Error(vmerr.CodeStackOverflow, "too many arguments copied into a closure continuation")
}

func parseCopyMore(raw uint64) (copyCount, more int) {
	copyCount = int((raw >> 4) & 0x0F)
	more = int(((raw + 1) & 0x0F) - 1)
	return copyCount, more
}

func encodeCopyMore(copyCount, more int) uint64 {
	return uint64(((copyCount & 0x0F) << 4) | (more & 0x0F))
}

func setContinuationArgsCommon(state *vm.State, copyCount, more int) error {
	if state.Stack.Len() < copyCount+1 {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		return err
	}

	if copyCount != 0 || more >= 0 {
		cont = vm.ForceControlData(cont)
		data := cont.GetControlData()

		if copyCount > 0 {
			if data.NumArgs >= 0 && data.NumArgs < copyCount {
				return closureStackOverflow()
			}

			if data.Stack == nil {
				data.Stack, err = state.Stack.SplitTop(copyCount, 0)
				if err != nil {
					return err
				}
			} else {
				if err = data.Stack.MoveFrom(state.Stack, copyCount); err != nil {
					return err
				}
			}

			if err = state.ConsumeStackGas(data.Stack); err != nil {
				return err
			}
			if data.NumArgs >= 0 {
				data.NumArgs -= copyCount
			}
		}

		if more >= 0 {
			switch {
			case data.NumArgs > more:
				data.NumArgs = invalidContinuationArgs
			case data.NumArgs < 0:
				data.NumArgs = more
			}
		}
	}

	return state.Stack.PushContinuation(cont)
}

func returnArgsCommon(state *vm.State, count int) error {
	if state.Stack.Len() < count {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	if state.Stack.Len() == count {
		return nil
	}

	copyCount := state.Stack.Len() - count
	altStack := state.Stack
	newStack, err := altStack.SplitTop(count, 0)
	if err != nil {
		return err
	}
	state.Stack = newStack

	cont := vm.ForceControlData(state.Reg.C[0])
	data := cont.GetControlData()
	if data.NumArgs >= 0 && data.NumArgs < copyCount {
		return closureStackOverflow()
	}

	if data.Stack == nil {
		data.Stack = altStack
	} else {
		if err = data.Stack.MoveFrom(altStack, copyCount); err != nil {
			return err
		}
	}

	if err = state.ConsumeStackGas(data.Stack); err != nil {
		return err
	}
	if data.NumArgs >= 0 {
		data.NumArgs -= copyCount
	}
	state.Reg.C[0] = cont
	return nil
}

func blessArgsCommon(state *vm.State, copyCount, more int) error {
	if state.Stack.Len() < copyCount+1 {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	code, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	stack := vm.NewStack()
	if copyCount > 0 {
		stack, err = state.Stack.SplitTop(copyCount, 0)
		if err != nil {
			return err
		}
	}
	if err = state.ConsumeStackGas(stack); err != nil {
		return err
	}

	return state.Stack.PushContinuation(&vm.OrdinaryContinuation{
		Data: vm.ControlData{
			Stack:   stack,
			NumArgs: more,
			CP:      state.CP,
		},
		Code: code,
	})
}

func SETCONTARGS(copyCount, more int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			return setContinuationArgsCommon(state, copyCount, more)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("SETCONTARGS %d,%d", copyCount, more)
		},
		BitPrefix: helpers.BytesPrefix(0xEC),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(encodeCopyMore(copyCount, more), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			raw, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			copyCount, more = parseCopyMore(raw)
			return nil
		},
	}
}

func RETURNARGS(count int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			return returnArgsCommon(state, count)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("RETURNARGS %d", count)
		},
		BitPrefix: helpers.SlicePrefix(12, []byte{0xED, 0x00}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(count), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			count = int(val)
			return nil
		},
	}
}

func RETURNVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return returnArgsCommon(state, int(count.Int64()))
		},
		Name:      "RETURNVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x10),
	}
}

func SETCONTVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			more, err := state.Stack.PopIntRange(-1, 255)
			if err != nil {
				return err
			}
			copyCount, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return setContinuationArgsCommon(state, int(copyCount.Int64()), int(more.Int64()))
		},
		Name:      "SETCONTVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x11),
	}
}

func SETNUMVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			more, err := state.Stack.PopIntRange(-1, 255)
			if err != nil {
				return err
			}
			return setContinuationArgsCommon(state, 0, int(more.Int64()))
		},
		Name:      "SETNUMVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x12),
	}
}

func BLESS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			code, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return state.Stack.PushContinuation(&vm.OrdinaryContinuation{
				Data: vm.ControlData{
					NumArgs: vm.ControlDataAllArgs,
					CP:      state.CP,
				},
				Code: code,
			})
		},
		Name:      "BLESS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x1E),
	}
}

func BLESSVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			more, err := state.Stack.PopIntRange(-1, 255)
			if err != nil {
				return err
			}
			copyCount, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return blessArgsCommon(state, int(copyCount.Int64()), int(more.Int64()))
		},
		Name:      "BLESSVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x1F),
	}
}

func BLESSARGS(copyCount, more int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			return blessArgsCommon(state, copyCount, more)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("BLESSARGS %d,%d", copyCount, more)
		},
		BitPrefix: helpers.BytesPrefix(0xEE),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(encodeCopyMore(copyCount, more), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			raw, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			copyCount, more = parseCopyMore(raw)
			return nil
		},
	}
}
