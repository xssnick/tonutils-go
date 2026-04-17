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
	vm.List = append(vm.List, func() vm.OP { return TRYARGS(0, 0) })
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

func TRYARGS(params, retvals int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			return executeTry(state, params, retvals)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("TRYARGS %d,%d", params, retvals)
		},
		BitPrefix: helpers.UIntPrefix(0xF3, 8),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(((params & 0x0F) << 4) | (retvals & 0x0F)), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			params = int((val >> 4) & 0x0F)
			retvals = int(val & 0x0F)
			return nil
		},
	}
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
	handler.GetControlData().Save.Define(2, oldC2)
	handler.GetControlData().Save.Define(0, cc)
	state.Reg.C[0] = cc
	state.Reg.C[2] = handler

	return state.Jump(cont)
}
