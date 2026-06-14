package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SETCONTCTRMANY(0) },
		func() vm.OP { return SETCONTCTRMANYX() },
	)
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
	return state.Stack.PushContinuation(cont)
}

func SETCONTCTRMANY(mask uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			return setContCtrManyCommon(state, mask)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("SETCONTCTRMANY %d", int(mask)+1)
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE3),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(mask), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			mask = uint8(val)
			return nil
		},
	}
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
		Name:      "SETCONTCTRMANYX",
		BitPrefix: helpers.BytesPrefix(0xED, 0xE4),
	}
}
