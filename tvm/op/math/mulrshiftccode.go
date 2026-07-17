package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTCCODE(1) })
}

func MULRSHIFTCCODE(value int) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
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
			if x == nil || y == nil {
				return pushMaybeInt(state, legacyRShiftNaNResult(state.GlobalVersion, uint64(imm()), cppRoundCeil), false)
			}

			q := helpers.DivCeil(x.Mul(x, y), y.Lsh(bigIntOne, uint(imm())))

			return state.Stack.PushInt(q)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0xB6),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d MULRSHIFTC#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
