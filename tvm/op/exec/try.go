package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TRY() })
	vm.ArgList = append(vm.ArgList, tryArgsOp)
}

func TRY() (op *helpers.SimpleOP) {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return executeTry(state, -1, -1)
		},
		Name:      "TRY",
		BitPrefix: helpers.BytesPrefix(0xF2, 0xFF),
	}
}

var tryArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xF3, 8)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		params, retvals := unpackArgPair(args)
		return executeTry(state, params, retvals)
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(8); err != nil {
			return 0, err
		}
		val, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		return packArgPair(int((val>>4)&0x0F), int(val&0x0F)), nil
	},
	Serializer: func(args uint64) *cell.Builder {
		params, retvals := unpackArgPair(args)
		return cell.BeginCell().
			MustStoreUInt(0xF3, 8).
			MustStoreUInt(uint64(((params&0x0F)<<4)|(retvals&0x0F)), 8)
	},
	Name: func(args uint64) string {
		params, retvals := unpackArgPair(args)
		return fmt.Sprintf("TRYARGS %d,%d", params, retvals)
	},
})

func TRYARGS(params, retvals int) vm.OP {
	return vm.Bind(tryArgsOp, packArgPair(params, retvals))
}

func executeTry(state *vm.State, params, retvals int) error {
	required := 2
	if params >= 0 {
		required += params
	}
	if state.Stack.Len() < required {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	handler, err := state.Stack.PopContinuation()
	if err != nil {
		return err
	}
	cont, err := state.Stack.PopContinuation()
	if err != nil {
		return err
	}

	oldC2 := state.Reg.C[2]
	cc, err := state.ExtractCurrentContinuation(7, params, retvals)
	if err != nil {
		return err
	}

	handler = vm.ForceControlData(handler)
	handler.GetControlData().Save.Define(2, cloneContinuation(oldC2))
	handler.GetControlData().Save.Define(0, cloneContinuation(cc))
	state.Reg.C[0] = cc
	state.Reg.C[2] = handler

	return state.Jump(cont)
}
