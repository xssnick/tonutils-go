package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return PUSHNAN() },
	)
	vm.ArgList = append(vm.ArgList, pushPow2Op, pushPow2DecOp, pushNegPow2Op)
}

// pushPowConst builds a PUSHPOW2-style opcode: the encoded byte holds the
// exponent minus one, so the pushed power covers 1..256. PUSHPOW2 spends its
// top encoding on PUSHNAN instead.
func pushPowConst(name string, prefix byte, nanAtMax bool, fn func(int) *big.Int) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(prefix)),
		ArgBits:  8,
		Name: func(args uint64) string {
			if nanAtMax && uint8(args) == 0xff {
				return "PUSHNAN"
			}
			return fmt.Sprintf("%s %d", name, bytePlusOneValue(args))
		},
		Action: func(state *vm.State, args uint64) error {
			if nanAtMax && uint8(args) == 0xff {
				return state.Stack.PushAny(vm.NaN{})
			}
			return state.Stack.PushInt(fn(bytePlusOneValue(args)))
		},
	})
}

var (
	pushPow2Op = pushPowConst("PUSHPOW2", 0x83, true, func(x int) *big.Int {
		return new(big.Int).Lsh(bigIntOne, uint(x))
	})
	pushPow2DecOp = pushPowConst("PUSHPOW2DEC", 0x84, false, func(x int) *big.Int {
		return new(big.Int).Sub(new(big.Int).Lsh(bigIntOne, uint(x)), bigIntOne)
	})
	pushNegPow2Op = pushPowConst("PUSHNEGPOW2", 0x85, false, func(x int) *big.Int {
		return new(big.Int).Neg(new(big.Int).Lsh(bigIntOne, uint(x)))
	})
)

func PUSHPOW2(value uint8) vm.OP {
	return vm.Bind(pushPow2Op, uint64(value))
}

func PUSHPOW2DEC(value uint8) vm.OP {
	return vm.Bind(pushPow2DecOp, uint64(value))
}

func PUSHNEGPOW2(value uint8) vm.OP {
	return vm.Bind(pushNegPow2Op, uint64(value))
}

func PUSHNAN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.PushAny(vm.NaN{})
		},
		Name:      "PUSHNAN",
		BitPrefix: helpers.BytesPrefix(0x83, 0xFF),
	}
}
