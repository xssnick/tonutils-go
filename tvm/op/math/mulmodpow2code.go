package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, mulModPow2CodeOp)
}

var mulModPow2CodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0xB8)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		if err := checkStackDepth(state, 2); err != nil {
			return err
		}
		y, err := popInt(state)
		if err != nil {
			return err
		}
		x, err := popInt(state)
		if err != nil {
			return err
		}
		if err = requireFiniteInts(y, x); err != nil {
			return err
		}

		q, _ := helpers.DivFloor(x.Mul(x, y), y.Lsh(bigIntOne, uint(bytePlusOneValue(args))))
		r := y.Sub(x, y.Mul(y, q))

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MULMODPOW2#", bytePlusOneValue(args))
	},
})

func MULMODPOW2CODE(value int) vm.OP {
	return vm.Bind(mulModPow2CodeOp, bytePlusOneArg(value))
}
