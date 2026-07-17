package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTCODE(1) })
}

func LSHIFTCODE(value int) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := popIntRead(state)
			if err != nil {
				return err
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, uint64(imm()), false), false)
			}

			return pushMaybeInt(state, leftShiftResult(x, uint64(imm())), false)
		},
		BitPrefix:       helpers.BytesPrefix(0xAA),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d LSHIFT#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
