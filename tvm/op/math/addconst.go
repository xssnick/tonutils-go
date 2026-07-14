package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ADDCONST(0) })
}

func ADDCONST(value int8) (op *helpers.AdvancedOP) {
	arg := big.NewInt(int64(value))
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
				return x.Add(x, arg)
			})
		},
		BitPrefix: helpers.BytesPrefix(0xA6),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("ADDINT %d", value)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadInt(8)
			if err != nil {
				return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
			}
			value = int8(val)
			arg.SetInt64(int64(value))
			return nil
		},
	}
	return op
}

func ADDINT(value int8) *helpers.AdvancedOP {
	return ADDCONST(value)
}
