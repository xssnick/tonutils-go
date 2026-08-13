package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, rshiftCodeModOp)
}

var rshiftCodeModOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x3C)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntFinite(state)
		if err != nil {
			return err
		}

		divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
		q := new(big.Int).Div(x, divider)
		r := x.Sub(x, new(big.Int).Mul(q, divider))

		err = state.Stack.PushInt(q)
		if err != nil {
			return err
		}

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d RSHIFT#MOD", bytePlusOneValue(args))
	},
})

func RSHIFTCODEMOD(value int) vm.OP {
	return vm.Bind(rshiftCodeModOp, bytePlusOneArg(value))
}
