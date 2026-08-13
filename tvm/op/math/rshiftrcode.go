package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, rshiftRCodeOp)
}

var rshiftRCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x35)),
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

		res := helpers.DivRound(x, new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args))))

		return state.Stack.PushInt(res)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d RSHIFTR#", bytePlusOneValue(args))
	},
})

func RSHIFTRCODE(value int) vm.OP {
	return vm.Bind(rshiftRCodeOp, bytePlusOneArg(value))
}
