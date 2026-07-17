package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTRCODE(1) })
}

func RSHIFTRCODE(value int) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := popIntRead(state)
			if err != nil {
				return err
			}
			if x == nil {
				if state.GlobalVersion >= 14 {
					return pushNaNOrOverflow(state, false)
				}
				return pushSmallInt(state, 0)
			}

			res := helpers.DivRound(x, new(big.Int).Lsh(bigIntOne, uint(imm())))

			return state.Stack.PushInt(res)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, 0x35),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d RSHIFTR#", imm())
		},
		DeserializeSuffix: deserializeImmediate,
	}
	return op
}
