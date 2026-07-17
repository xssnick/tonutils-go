package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return RSHIFTFLOOR() },
		func() vm.OP { return RSHIFTCODEFLOOR(1) },
	)
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

func RSHIFTCODEFLOOR(value int) *helpers.AdvancedOP {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := popInt(state)
			if err != nil {
				return err
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, uint64(imm()), true), false)
			}

			return state.Stack.PushInt(x.Rsh(x, uint(imm())))
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0x34),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFT#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
}
