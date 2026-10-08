package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return RSHIFTFLOOR() },
	)
	vm.ArgList = append(vm.ArgList, rshiftCodeFloorOp)
}

func RSHIFTFLOOR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			x, err := popInt(state)
			if err != nil {
				return err
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, y.Uint64(), true), false)
			}

			return state.Stack.PushInt(x.Rsh(x, uint(y.Uint64())))
		},
		Name:      "RSHIFT",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x24),
	}
}

var rshiftCodeFloorOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, 0x34)),
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

func RSHIFTCODEFLOOR(value int) vm.OP {
	return vm.Bind(rshiftCodeFloorOp, bytePlusOneArg(value))
}
