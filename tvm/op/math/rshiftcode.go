package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, rshiftCodeOp)
}

var rshiftCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xAB)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popInt(state)
		if err != nil {
			return err
		}

		shift := bytePlusOneValue(args)
		if x == nil {
			return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, uint64(shift), true), false)
		}

		return state.Stack.PushInt(x.Rsh(x, uint(shift)))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d RSHIFT#", bytePlusOneValue(args))
	},
})

func RSHIFTCODE(value int) vm.OP {
	return vm.Bind(rshiftCodeOp, bytePlusOneArg(value))
}
