package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTCODE(0) })
}

func LSHIFTCODE(value int8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushInt(x.Lsh(x, uint(value)))
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xAA}, 8).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d LSHIFT#", value)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			value = int8(val) + 1 // x - x*2^(cc+1)
			return nil
		},
	}
	return op
}
