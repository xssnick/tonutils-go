package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETCONTCTRMANYX() })
	vm.ArgList = append(vm.ArgList, setContCtrManyOp)
}

func setContCtrManyCommon(state *vm.State, mask uint8) error {
	if mask&(1<<6) != 0 {
		return vmerr.Error(vmerr.CodeRangeCheck, "no control register c6")
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		return err
	}
	cont = vm.ForceControlData(cont)
	data := cont.GetControlData()
	for i, m := 0, mask; m != 0; i, m = i+1, m>>1 {
		if m&1 == 0 {
			continue
		}
		if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(state.Reg.Get(i))) {
			return vmerr.Error(vmerr.CodeTypeCheck)
		}
	}
	return state.Stack.PushOwnedContinuation(cont)
}

var setContCtrManyOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed:   helpers.SinglePrefixed(helpers.BytesPrefix(0xED, 0xE3)),
	ArgBits:    8,
	MinVersion: 9,
	Action: func(state *vm.State, args uint64) error {
		return setContCtrManyCommon(state, uint8(args))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("SETCONTCTRMANY %d", int(uint8(args))+1)
	},
})

func SETCONTCTRMANY(mask uint8) vm.OP {
	return vm.Bind(setContCtrManyOp, uint64(mask))
}

func SETCONTCTRMANYX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			mask, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return setContCtrManyCommon(state, uint8(mask))
		},
		Name:       "SETCONTCTRMANYX",
		BitPrefix:  helpers.BytesPrefix(0xED, 0xE4),
		MinVersion: 9,
	}
}
