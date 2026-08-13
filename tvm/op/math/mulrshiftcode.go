package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, mulRShiftCodeOp)
}

var mulRShiftCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0xB4)),
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
		shift := uint64(bytePlusOneValue(args))
		if x == nil || y == nil {
			return pushMaybeInt(state, legacyRShiftNaNResult(state.GlobalVersion, shift, cppRoundFloor), false)
		}

		q, _ := helpers.DivFloor(x.Mul(x, y), y.Lsh(bigIntOne, uint(shift)))

		return state.Stack.PushInt(q)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d MULRSHIFT#", bytePlusOneValue(args))
	},
})

func MULRSHIFTCODE(value int) vm.OP {
	return vm.Bind(mulRShiftCodeOp, bytePlusOneArg(value))
}
