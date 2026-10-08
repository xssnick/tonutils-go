package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, rshiftRCodeModOp)
}

var rshiftRCodeModOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x3D)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntFinite(state)
		if err != nil {
			return err
		}

		divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
		q := helpers.DivRound(x, divider)
		r := x.Sub(x, new(big.Int).Mul(q, divider))

		err = state.Stack.PushInt(q)
		if err != nil {
			return err
		}

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d RSHIFTR#MOD", bytePlusOneValue(args))
	},
})

func RSHIFTRCODEMOD(value int) vm.OP {
	return vm.Bind(rshiftRCodeModOp, bytePlusOneArg(value))
}
