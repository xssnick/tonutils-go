package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTCODE(1) })
}

func RSHIFTCODE(value int) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
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
		BitPrefix:       helpers.BytesPrefix(0xAB),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFT#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
