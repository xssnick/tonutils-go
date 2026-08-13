package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		newLshiftDivCodeOp("LSHIFTADDDIVMOD#", 0xD0, 0, 0),
		newLshiftDivCodeOp("LSHIFTADDDIVMODR#", 0xD1, 0, 1),
		newLshiftDivCodeOp("LSHIFTADDDIVMODC#", 0xD2, 0, 2),
		newLshiftDivCodeOp("LSHIFTDIV#", 0xD4, 1, 0),
		newLshiftDivCodeOp("LSHIFTDIVR#", 0xD5, 1, 1),
		newLshiftDivCodeOp("LSHIFTDIVC#", 0xD6, 1, 2),
		newLshiftDivCodeOp("LSHIFTMOD#", 0xD8, 2, 0),
		newLshiftDivCodeOp("LSHIFTMODR#", 0xD9, 2, 1),
		newLshiftDivCodeOp("LSHIFTMODC#", 0xDA, 2, 2),
		newLshiftDivCodeOp("LSHIFTDIVMOD#", 0xDC, 3, 0),
		newLshiftDivCodeOp("LSHIFTDIVMODR#", 0xDD, 3, 1),
		newLshiftDivCodeOp("LSHIFTDIVMODC#", 0xDE, 3, 2),
	)
}

func newLshiftDivCodeOp(name string, op byte, d int, roundMode int) *helpers.ArgOP {
	out := &helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA9, op)),
		ArgBits:  8,
		Action: func(state *vm.State, args uint64) error {
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
			shift := uint64(bytePlusOneValue(args))
			x = legacyLeftShiftOperand(state.GlobalVersion, x, shift)
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

			dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift))
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
		Name: func(args uint64) string {
			return fmt.Sprintf("%d %s", bytePlusOneValue(args), name)
		},
	}
	if d == 0 {
		out.MinVersion = 4
	}
	return helpers.NewArgOP(out)
}

func lshiftDivCodeOp(name string, op byte, d int, roundMode int, value int) vm.OP {
	return vm.Bind(newLshiftDivCodeOp(name, op, d, roundMode), bytePlusOneArg(value))
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
