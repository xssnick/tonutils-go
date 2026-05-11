package cellslice

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STU(0) })
}

func STU(sz uint) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			b0, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			i1, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			if err := b0.StoreBigUInt(i1, sz); err != nil {
				return err
			}
			return state.Stack.PushBuilder(b0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d STU", sz)
		},
		BitPrefix: helpers.BytesPrefix(0xCB),
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
