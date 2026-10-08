package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, lshiftCodeOp)
}

var lshiftCodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xAA)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, err := popIntRead(state)
		if err != nil {
			return err
		}

		shift := uint64(bytePlusOneValue(args))
		if x == nil {
			return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, shift, false), false)
		}

		return pushMaybeInt(state, leftShiftResult(x, shift), false)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d LSHIFT#", bytePlusOneValue(args))
	},
})

func LSHIFTCODE(value int) vm.OP {
	return vm.Bind(lshiftCodeOp, bytePlusOneArg(value))
}
