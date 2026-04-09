package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTCCODE(0) })
}

func RSHIFTCCODE(value int8) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			res := helpers.DivCeil(x, new(big.Int).Lsh(big.NewInt(1), uint(imm())))

			return state.Stack.PushInt(res)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0x36),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFTC#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
