package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CALLXARGS(0, 0) })
	vm.List = append(vm.List, func() vm.OP { return CALLXARGSP(0) })
}

func CALLXARGS(params, retvals int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			return state.CallArgs(cont, params, retvals)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("CALLXARGS %d,%d", params, retvals)
		},
		BitPrefix: helpers.BytesPrefix(0xDA),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64((params<<4)|retvals), 8)
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

func CALLXARGSP(params int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			return state.CallArgs(cont, params, -1)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("CALLXARGS %d,-1", params)
		},
		BitPrefix: helpers.SlicePrefix(12, []byte{0xDB, 0x00}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(params), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			params = int(val)
			return nil
		},
	}
}
