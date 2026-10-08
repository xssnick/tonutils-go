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
		func() vm.OP { return RETURNVARARGS() },
		func() vm.OP { return SETCONTVARARGS() },
		func() vm.OP { return SETNUMVARARGS() },
		func() vm.OP { return BLESS() },
		func() vm.OP { return BLESSVARARGS() },
	)
	vm.ArgList = append(vm.ArgList, setContArgsOp, returnArgsOp, blessArgsOp)
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

	return state.Stack.PushOwnedContinuation(cont)
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

	cont := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
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

	return state.Stack.PushOwnedContinuation(&vm.OrdinaryContinuation{
		Data: vm.ControlData{
			Stack:   stack,
			NumArgs: more,
			CP:      state.CP,
		},
		Code: code,
	})
}

// decodeCopyMore reads the packed copy/more byte that SETCONTARGS, BLESSARGS
// and CALLCCARGS share.
func decodeCopyMore(prefixBits uint) func(*vm.State, *cell.Slice) (uint64, error) {
	return func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(prefixBits); err != nil {
			return 0, err
		}
		raw, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		return packArgPair(parseCopyMore(raw)), nil
	}
}

func serializeCopyMore(prefix helpers.BitPrefix) func(uint64) *cell.Builder {
	return func(args uint64) *cell.Builder {
		copyCount, more := unpackArgPair(args)
		return cell.BeginCell().
			MustStoreSlice(prefix.Data, prefix.Bits).
			MustStoreUInt(encodeCopyMore(copyCount, more), 8)
	}
}

var setContArgsPrefix = helpers.BytesPrefix(0xEC)

var setContArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(setContArgsPrefix),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		copyCount, more := unpackArgPair(args)
		return setContinuationArgsCommon(state, copyCount, more)
	},
	Decode:     decodeCopyMore(setContArgsPrefix.Bits),
	Serializer: serializeCopyMore(setContArgsPrefix),
	Name: func(args uint64) string {
		copyCount, more := unpackArgPair(args)
		return fmt.Sprintf("SETCONTARGS %d,%d", copyCount, more)
	},
})

var returnArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(12, []byte{0xED, 0x00})),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		return returnArgsCommon(state, int(int32(args)))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("RETURNARGS %d", int32(args))
	},
})

func SETCONTARGS(copyCount, more int) vm.OP {
	return vm.Bind(setContArgsOp, packArgPair(copyCount, more))
}

func RETURNARGS(count int) vm.OP {
	return vm.Bind(returnArgsOp, uint64(uint32(count)))
}

func RETURNVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return returnArgsCommon(state, int(count))
		},
		Name:      "RETURNVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x10),
	}
}

func SETCONTVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			more, err := state.Stack.PopIntRangeInt64(-1, 255)
			if err != nil {
				return err
			}
			copyCount, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return setContinuationArgsCommon(state, int(copyCount), int(more))
		},
		Name:      "SETCONTVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x11),
	}
}

func SETNUMVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			more, err := state.Stack.PopIntRangeInt64(-1, 255)
			if err != nil {
				return err
			}
			return setContinuationArgsCommon(state, 0, int(more))
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
			return state.Stack.PushOwnedContinuation(&vm.OrdinaryContinuation{
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
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			more, err := state.Stack.PopIntRangeInt64(-1, 255)
			if err != nil {
				return err
			}
			copyCount, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return blessArgsCommon(state, int(copyCount), int(more))
		},
		Name:      "BLESSVARARGS",
		BitPrefix: helpers.BytesPrefix(0xED, 0x1F),
	}
}

var blessArgsPrefix = helpers.BytesPrefix(0xEE)

var blessArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(blessArgsPrefix),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		copyCount, more := unpackArgPair(args)
		return blessArgsCommon(state, copyCount, more)
	},
	Decode:     decodeCopyMore(blessArgsPrefix.Bits),
	Serializer: serializeCopyMore(blessArgsPrefix),
	Name: func(args uint64) string {
		copyCount, more := unpackArgPair(args)
		return fmt.Sprintf("BLESSARGS %d,%d", copyCount, more)
	},
})

func BLESSARGS(copyCount, more int) vm.OP {
	return vm.Bind(blessArgsOp, packArgPair(copyCount, more))
}
