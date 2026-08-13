package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, mulAddRShiftCodeModOp)
}

var mulAddRShiftCodeModOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed:   helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0xB0)),
	ArgBits:    8,
	MinVersion: 4,
	Action: func(state *vm.State, args uint64) error {
		if err := checkStackDepth(state, 3); err != nil {
			return err
		}
		w, err := popInt(state)
		if err != nil {
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
		if err = requireFiniteInts(w, y, x); err != nil {
			return err
		}

		dividend := x.Add(x.Mul(x, y), w)
		q, _ := helpers.DivFloor(dividend, y.Lsh(bigIntOne, uint(bytePlusOneValue(args))))
		r := w.Sub(dividend, y.Mul(y, q))

		err = state.Stack.PushInt(q)
		if err != nil {
			return err
		}

		return state.Stack.PushInt(r)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MULADDRSHIFT#MOD", bytePlusOneValue(args))
	},
})

func MULADDRSHIFTCODEMOD(value int) vm.OP {
	return vm.Bind(mulAddRShiftCodeModOp, bytePlusOneArg(value))
}
