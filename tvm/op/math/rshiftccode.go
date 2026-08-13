package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, rshiftCCodeOp)
}

var rshiftCCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x36)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntRead(state)
		if err != nil {
			return err
		}
		if x == nil {
			if state.GlobalVersion >= 14 {
				return pushNaNOrOverflow(state, false)
			}
			return pushSmallInt(state, 0)
		}

		res := helpers.DivCeil(x, new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args))))

		return state.Stack.PushInt(res)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d RSHIFTC#", bytePlusOneValue(args))
	},
})

func RSHIFTCCODE(value int) vm.OP {
	return vm.Bind(rshiftCCodeOp, bytePlusOneArg(value))
}
