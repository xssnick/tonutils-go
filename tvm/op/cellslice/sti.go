package cellslice

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STI(0) })
}

func STI(sz uint) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			b0, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			i1, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			if err := b0.StoreBigInt(i1.ToBigInt(), sz); err != nil {
				return err
			}
			return state.Stack.Push(b0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d STI", sz)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xCA}, 8).EndCell(),
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
