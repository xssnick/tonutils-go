package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CALLCC() })
	vm.List = append(vm.List, func() vm.OP { return CALLCCARGS(0, -1) })
	vm.List = append(vm.List, func() vm.OP { return CALLXVARARGS() })
	vm.List = append(vm.List, func() vm.OP { return RETVARARGS() })
	vm.List = append(vm.List, func() vm.OP { return CALLCCVARARGS() })
}

func CALLCC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cc, err := state.ExtractCurrentContinuation(3, -1, -1)
			if err != nil {
				return err
			}

			if err = state.Stack.PushContinuation(cc); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:      "CALLCC",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x34),
	}
}

func CALLCCARGS(params, retvals int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			if state.Stack.Len() < params+1 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cc, err := state.ExtractCurrentContinuation(3, params, retvals)
			if err != nil {
				return err
			}

			if err = state.Stack.PushContinuation(cc); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("CALLCCARGS %d,%d", params, retvals)
		},
		BitPrefix: helpers.BytesPrefix(0xDB, 0x36),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(encodeCopyMore(params, retvals), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			params, retvals = parseCopyMore(val)
			return nil
		},
	}
}

func CALLXVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			retvals, err := state.Stack.PopIntRangeInt64(-1, 254)
			if err != nil {
				return err
			}

			params, err := state.Stack.PopIntRangeInt64(-1, 254)
			if err != nil {
				return err
			}

			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			return state.CallArgs(cont, int(params), int(retvals))
		},
		Name:      "CALLXVARARGS",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x38),
	}
}

func RETVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			retvals, err := state.Stack.PopIntRangeInt64(-1, 254)
			if err != nil {
				return err
			}
			return state.Return(int(retvals))
		},
		Name:      "RETVARARGS",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x39),
	}
}

func CALLCCVARARGS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			retvals, err := state.Stack.PopIntRangeInt64(-1, 254)
			if err != nil {
				return err
			}

			paramsVal, err := state.Stack.PopIntRangeInt64(-1, 254)
			if err != nil {
				return err
			}

			params := int(paramsVal)
			if params >= 0 && state.Stack.Len() < params+1 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cc, err := state.ExtractCurrentContinuation(3, params, int(retvals))
			if err != nil {
				return err
			}

			if err = state.Stack.PushContinuation(cc); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:      "CALLCCVARARGS",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x3B),
	}
}
