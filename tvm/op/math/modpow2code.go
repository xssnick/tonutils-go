package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, modPow2CodeOp)
}

var modPow2CodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x38)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntFinite(state)
		if err != nil {
			return err
		}

		divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
		q := new(big.Int).Div(x, divider)
		r := x.Sub(x, q.Mul(q, divider))

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MODPOW2#", bytePlusOneValue(args))
	},
})

func MODPOW2CODE(value int) vm.OP {
	return vm.Bind(modPow2CodeOp, bytePlusOneArg(value))
}
