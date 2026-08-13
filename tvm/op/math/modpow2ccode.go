package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, modPow2CCodeOp)
}

var modPow2CCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x3A)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntFinite(state)
		if err != nil {
			return err
		}

		divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
		q := helpers.DivCeil(x, divider)
		r := x.Sub(x, q.Mul(q, divider))

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MODPOW2C#", bytePlusOneValue(args))
	},
})

func MODPOW2CCODE(value int) vm.OP {
	return vm.Bind(modPow2CCodeOp, bytePlusOneArg(value))
}
