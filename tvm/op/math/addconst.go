package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ADDCONST(0) })
}

func ADDCONST(value int8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushInt(i0.Add(i0, big.NewInt(int64(value))))
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xA6}, 8).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d ADDCONST", value)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadInt(8)
			if err != nil {
				return err
			}
			value = int8(val)
			return nil
		},
	}
	return op
}
