package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		addrShiftModOp,
		addrShiftRModOp,
		addrShiftCModOp,
	)
}

func newAddrShiftCodeModOp(name string, prefix helpers.BitPrefix, round func(*big.Int, *big.Int) (*big.Int, *big.Int)) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed:   helpers.SinglePrefixed(prefix),
		ArgBits:    8,
		MinVersion: 4,
		Action: func(state *vm.State, args uint64) error {
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
			divider := new(big.Int).Lsh(bigIntOne, uint(bytePlusOneValue(args)))
			q, r := round(dividend, divider)

			if err = state.Stack.PushInt(q); err != nil {
				return err
			}
			return state.Stack.PushInt(r)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("%d %s", bytePlusOneValue(args), name)
		},
	})
}

func addrShiftCodeModOp(name string, prefix helpers.BitPrefix, value int, round func(*big.Int, *big.Int) (*big.Int, *big.Int)) vm.OP {
	return vm.Bind(newAddrShiftCodeModOp(name, prefix, round), bytePlusOneArg(value))
}

var (
	addrShiftModOp = newAddrShiftCodeModOp("ADDRSHIFT#MOD", helpers.BytesPrefix(0xA9, 0x30), helpers.DivFloor)

	addrShiftRModOp = newAddrShiftCodeModOp("ADDRSHIFTR#MOD", helpers.BytesPrefix(0xA9, 0x31), func(x, y *big.Int) (*big.Int, *big.Int) {
		q := helpers.DivRound(x, y)
		return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
	})

	addrShiftCModOp = newAddrShiftCodeModOp("ADDRSHIFTC#MOD", helpers.BytesPrefix(0xA9, 0x32), func(x, y *big.Int) (*big.Int, *big.Int) {
		q := helpers.DivCeil(x, y)
		return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
	})
)

func ADDRSHIFTCODEMOD(value int) vm.OP {
	return vm.Bind(addrShiftModOp, bytePlusOneArg(value))
}

func ADDRSHIFTRCODEMOD(value int) vm.OP {
	return vm.Bind(addrShiftRModOp, bytePlusOneArg(value))
}

func ADDRSHIFTCCODEMOD(value int) vm.OP {
	return vm.Bind(addrShiftCModOp, bytePlusOneArg(value))
}
