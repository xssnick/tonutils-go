package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTRCODE(0) })
}

func RSHIFTRCODE(value int8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			res := helpers.DivRound(x, new(big.Int).Lsh(big.NewInt(1), uint(value)))

			return state.Stack.PushInt(res)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xA9, 0x35}, 16).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFTR#", value)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			value = int8(val) + 1
			return nil
		},
	}
	return op
}
