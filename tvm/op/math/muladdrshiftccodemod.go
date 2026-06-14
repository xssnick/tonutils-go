package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULADDRSHIFTCCODEMOD(0) })
}

func MULADDRSHIFTCCODEMOD(value int8) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
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
			q := helpers.DivCeil(dividend, y.Lsh(bigIntOne, uint(imm())))
			r := w.Sub(dividend, y.Mul(y, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0xB2),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d MULADDRSHIFTC#MOD", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
