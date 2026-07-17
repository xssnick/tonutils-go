package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return ADDRSHIFTCODEMOD(1) },
		func() vm.OP { return ADDRSHIFTRCODEMOD(1) },
		func() vm.OP { return ADDRSHIFTCCODEMOD(1) },
	)
}

func addrShiftCodeModOp(name string, prefix helpers.BitPrefix, value int, round func(*big.Int, *big.Int) (*big.Int, *big.Int)) *helpers.AdvancedOP {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		MinVersion:    4,
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			w, err := popIntRead(state)
			if err != nil {
				return err
			}
			x, err := popIntRead(state)
			if err != nil {
				return err
			}
			if err = requireFiniteInts(w, x); err != nil {
				return err
			}

			dividend := new(big.Int).Add(x, w)
			divider := new(big.Int).Lsh(bigIntOne, uint(imm()))
			q, r := round(dividend, divider)

			if err = state.Stack.PushInt(q); err != nil {
				return err
			}
			return state.Stack.PushInt(r)
		},
		BitPrefix:       prefix,
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d %s", imm(), name)
		},
		DeserializeSuffix: deserializeImmediate,
	}
}

func ADDRSHIFTCODEMOD(value int) *helpers.AdvancedOP {
	return addrShiftCodeModOp("ADDRSHIFT#MOD", helpers.BytesPrefix(0xA9, 0x30), value, helpers.DivFloor)
}

func ADDRSHIFTRCODEMOD(value int) *helpers.AdvancedOP {
	return addrShiftCodeModOp("ADDRSHIFTR#MOD", helpers.BytesPrefix(0xA9, 0x31), value, func(x, y *big.Int) (*big.Int, *big.Int) {
		q := helpers.DivRound(x, y)
		return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
	})
}

func ADDRSHIFTCCODEMOD(value int) *helpers.AdvancedOP {
	return addrShiftCodeModOp("ADDRSHIFTC#MOD", helpers.BytesPrefix(0xA9, 0x32), value, func(x, y *big.Int) (*big.Int, *big.Int) {
		q := helpers.DivCeil(x, y)
		return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
	})
}
