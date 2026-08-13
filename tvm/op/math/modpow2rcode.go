package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, modPow2RCodeOp)
}

var modPow2RCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x39)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntFinite(state)
		if err != nil {
			return err
		}

		divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
		q := helpers.DivRound(x, divider)
		r := x.Sub(x, q.Mul(q, divider))

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MODPOW2R#", bytePlusOneValue(args))
	},
})

func MODPOW2RCODE(value int) vm.OP {
	return vm.Bind(modPow2RCodeOp, bytePlusOneArg(value))
}
