package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CALLCC() })
	vm.ArgList = append(vm.ArgList, callCCArgsOp)
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

			if err = state.Stack.PushOwnedContinuation(cc); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:      "CALLCC",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x34),
	}
}

var callCCArgsPrefix = helpers.BytesPrefix(0xDB, 0x36)

var callCCArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(callCCArgsPrefix),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		params, retvals := unpackArgPair(args)

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

		if err = state.Stack.PushOwnedContinuation(cc); err != nil {
			return err
		}

		return state.Jump(cont)
	},
	Decode:     decodeCopyMore(callCCArgsPrefix.Bits),
	Serializer: serializeCopyMore(callCCArgsPrefix),
	Name: func(args uint64) string {
		params, retvals := unpackArgPair(args)
		return fmt.Sprintf("CALLCCARGS %d,%d", params, retvals)
	},
})

func CALLCCARGS(params, retvals int) vm.OP {
	return vm.Bind(callCCArgsOp, packArgPair(params, retvals))
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

			if err = state.Stack.PushOwnedContinuation(cc); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:      "CALLCCVARARGS",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x3B),
	}
}
