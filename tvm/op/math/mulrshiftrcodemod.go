package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, mulRShiftRCodeModOp)
}

var mulRShiftRCodeModOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0xBD)),
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

		q := helpers.DivRound(x.Mul(x, y), y.Lsh(bigIntOne, uint(bytePlusOneValue(args))))
		r := y.Sub(x, y.Mul(y, q))

		err = state.Stack.PushInt(q)
		if err != nil {
			return err
		}

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MULRSHIFTR#MOD", bytePlusOneValue(args))
	},
})

func MULRSHIFTRCODEMOD(value int) vm.OP {
	return vm.Bind(mulRShiftRCodeModOp, bytePlusOneArg(value))
}
