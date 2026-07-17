package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTCCODEMOD(1) })
}

func RSHIFTCCODEMOD(value int) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := popIntFinite(state)
			if err != nil {
				return err
			}

			divider := new(big.Int).Lsh(bigIntOne, uint(imm()))
			q := helpers.DivCeil(x, divider)
			r := x.Sub(x, new(big.Int).Mul(q, divider))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0x3E),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFTC#MOD", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
