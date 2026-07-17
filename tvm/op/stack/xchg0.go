package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCHG0(0) })
	for i := uint64(2); i < 16; i++ {
		value := i
		vm.List = append(vm.List, func() vm.OP {
			return helpers.FullOpcodeVariant(XCHG0(uint8(value)), helpers.UIntPrefix(value, 8))
		})
	}
}

// constant prefix, computed once instead of on every decode
var xchg0Prefix = helpers.SlicePrefix(4, []byte{0x0})

func XCHG0(i uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			return state.Stack.Exchange(int(0), int(i))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d XCHG0", i)
		},
		BitPrefix: xchg0Prefix,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(val)

			return nil
		},
	}
	return op
}
