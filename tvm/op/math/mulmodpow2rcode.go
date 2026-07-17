package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULMODPOW2RCODE(1) })
}

func MULMODPOW2RCODE(value int) (op *helpers.AdvancedOP) {
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
			if err = requireFiniteInts(y, x); err != nil {
				return err
			}

			q := helpers.DivRound(x.Mul(x, y), y.Lsh(bigIntOne, uint(imm())))
			r := y.Sub(x, y.Mul(y, q))

			return state.Stack.PushInt(r)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0xB9),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d MULMODPOW2R#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
