package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return lshiftDivCodeOp("LSHIFTADDDIVMOD#", 0xD0, 0, 0, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTADDDIVMODR#", 0xD1, 0, 1, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTADDDIVMODC#", 0xD2, 0, 2, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIV#", 0xD4, 1, 0, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIVR#", 0xD5, 1, 1, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIVC#", 0xD6, 1, 2, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTMOD#", 0xD8, 2, 0, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTMODR#", 0xD9, 2, 1, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTMODC#", 0xDA, 2, 2, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIVMOD#", 0xDC, 3, 0, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIVMODR#", 0xDD, 3, 1, 1) },
		func() vm.OP { return lshiftDivCodeOp("LSHIFTDIVMODC#", 0xDE, 3, 2, 1) },
	)
}

func lshiftDivCodeOp(name string, op byte, d int, roundMode int, value int) *helpers.AdvancedOP {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	out := &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			required := 2
			if d == 0 {
				required = 3
			}
			if err := checkStackDepth(state, required); err != nil {
				return err
			}

			z, err := popIntRead(state)
			if err != nil {
				return err
			}

			var w *big.Int
			if d == 0 {
				w, err = popIntRead(state)
				if err != nil {
					return err
				}
			}

			x, err := popIntRead(state)
			if err != nil {
				return err
			}
			x = legacyLeftShiftOperand(state.GlobalVersion, x, uint64(imm()))
			if d == 0 {
				if err = requireFiniteInts(z, w, x); err != nil {
					return err
				}
			} else if err = requireFiniteInts(z, x); err != nil {
				return err
			}
			if z.Sign() == 0 {
				return vmerr.Error(vmerr.CodeIntOverflow, "division by zero")
			}

			dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(imm()))
			if d == 0 {
				dividend.Add(dividend, w)
			}

			q, r := roundDivMod(dividend, z, roundMode)
			if d == 1 {
				return state.Stack.PushInt(q)
			}
			if d == 2 {
				return state.Stack.PushInt(r)
			}
			if err = state.Stack.PushInt(q); err != nil {
				return err
			}
			return state.Stack.PushInt(r)
		},
		BitPrefix:       helpers.BytesPrefix(0xA9, op),
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d %s", imm(), name)
		},
		DeserializeSuffix: deserializeImmediate,
	}
	if d == 0 {
		out.MinVersion = 4
	}
	return out
}

func roundDivMod(x, y *big.Int, roundMode int) (*big.Int, *big.Int) {
	if roundMode == 0 {
		return helpers.DivFloor(x, y)
	}

	var q *big.Int
	if roundMode == 1 {
		q = helpers.DivRound(x, y)
	} else {
		q = helpers.DivCeil(x, y)
	}
	return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
}
