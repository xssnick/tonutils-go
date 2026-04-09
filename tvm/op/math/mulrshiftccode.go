package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTCCODE(0) })
}

func MULRSHIFTCCODE(value int8) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			q := helpers.DivCeil(x.Mul(x, y), y.Lsh(big.NewInt(1), uint(imm())))

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
