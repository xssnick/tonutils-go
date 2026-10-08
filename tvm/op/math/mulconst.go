package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, mulConstOp)
}

var mulConstOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA7)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i0, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		arg := big.NewInt(int64(int8(args)))
		return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
			return x.Mul(x, arg)
		})
	},
	Serializer: func(args uint64) *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0xA7, 8).MustStoreInt(int64(int8(args)), 8)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("MULINT %d", int8(args))
	},
})

func MULCONST(value int8) vm.OP {
	return vm.Bind(mulConstOp, uint64(uint8(value)))
}

func MULINT(value int8) vm.OP {
	return MULCONST(value)
}
