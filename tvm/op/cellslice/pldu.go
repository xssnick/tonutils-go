package cellslice

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return PLDU(0) })
}

func PLDU(sz uint) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			i, err := s0.PreloadBigUInt(sz)
			if err != nil {
				return err
			}

			err = state.Stack.PushInt(i)
			if err != nil {
				return err
			}

			return state.Stack.PushSlice(s0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d PLDU", sz)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xD7, 0x0B}, 16).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(sz-1), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			sz = uint(val) + 1
			return nil
		},
	}
	return op
}
