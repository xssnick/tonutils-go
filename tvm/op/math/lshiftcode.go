package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTCODE(0) })
}

func LSHIFTCODE(value int8) (op *helpers.AdvancedOP) {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := popInt(state)
			if err != nil {
				return err
			}
			if x == nil {
				if state.GlobalVersion >= 14 {
					return pushNaNOrOverflow(state, false)
				}
				return pushSmallInt(state, 0)
			}

			return state.Stack.PushInt(x.Lsh(x, uint(imm())))
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
