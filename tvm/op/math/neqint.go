package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return NEQINT(0) })
}

func NEQINT(value int8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushBool(i0.Cmp(big.NewInt(int64(value))) != 0)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xC3}, 8).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d NEQINT", value)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			value = int8(val)
			return nil
		},
	}
	return op
}
