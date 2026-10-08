package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return QADD() },
		func() vm.OP { return QSUB() },
		func() vm.OP { return QSUBR() },
		func() vm.OP { return QNEGATE() },
		func() vm.OP { return QINC() },
		func() vm.OP { return QDEC() },
		func() vm.OP { return QMUL() },
	)
	vm.ArgList = append(vm.ArgList, qAddIntOp, qMulIntOp)
}

func quietUnaryIntOp(name string, prefix helpers.BitPrefix, fn func(*big.Int) *big.Int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 1); err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushOwnedIntQuiet(fn(x))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietBinaryIntOp(name string, prefix helpers.BitPrefix, fn func(x, y *big.Int) *big.Int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil || y == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushOwnedIntQuiet(fn(x, y))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietTinyIntOp(name string, prefix helpers.BitPrefix, fn func(x, arg *big.Int) *big.Int) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(prefix),
		ArgBits:  8,
		Action: func(state *vm.State, args uint64) error {
			if err := checkStackDepth(state, 1); err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushOwnedIntQuiet(fn(x, big.NewInt(int64(int8(args)))))
		},
		Serializer: func(args uint64) *cell.Builder {
			return cell.BeginCell().
				MustStoreSlice(prefix.Data, prefix.Bits).
				MustStoreInt(int64(int8(args)), 8)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("%s %d", name, int8(args))
		},
	})
}

func QADD() *helpers.SimpleOP {
	return quietBinaryIntOp("QADD", helpers.BytesPrefix(0xB7, 0xA0), func(x, y *big.Int) *big.Int {
		return x.Add(x, y)
	})
}

func QSUB() *helpers.SimpleOP {
	return quietBinaryIntOp("QSUB", helpers.BytesPrefix(0xB7, 0xA1), func(x, y *big.Int) *big.Int {
		return x.Sub(x, y)
	})
}

func QSUBR() *helpers.SimpleOP {
	return quietBinaryIntOp("QSUBR", helpers.BytesPrefix(0xB7, 0xA2), func(x, y *big.Int) *big.Int {
		return y.Sub(y, x)
	})
}

func QNEGATE() *helpers.SimpleOP {
	return quietUnaryIntOp("QNEGATE", helpers.BytesPrefix(0xB7, 0xA3), func(x *big.Int) *big.Int {
		return x.Neg(x)
	})
}

func QINC() *helpers.SimpleOP {
	return quietUnaryIntOp("QINC", helpers.BytesPrefix(0xB7, 0xA4), func(x *big.Int) *big.Int {
		return x.Add(x, bigIntOne)
	})
}

func QDEC() *helpers.SimpleOP {
	return quietUnaryIntOp("QDEC", helpers.BytesPrefix(0xB7, 0xA5), func(x *big.Int) *big.Int {
		return x.Sub(x, bigIntOne)
	})
}

var (
	qAddIntOp = quietTinyIntOp("QADDINT", helpers.BytesPrefix(0xB7, 0xA6), func(x, arg *big.Int) *big.Int {
		return x.Add(x, arg)
	})
	qMulIntOp = quietTinyIntOp("QMULINT", helpers.BytesPrefix(0xB7, 0xA7), func(x, arg *big.Int) *big.Int {
		return x.Mul(x, arg)
	})
)

func QADDINT(value int8) vm.OP {
	return vm.Bind(qAddIntOp, uint64(uint8(value)))
}

func QMULINT(value int8) vm.OP {
	return vm.Bind(qMulIntOp, uint64(uint8(value)))
}

func QMUL() *helpers.SimpleOP {
	return quietBinaryIntOp("QMUL", helpers.BytesPrefix(0xB7, 0xA8), func(x, y *big.Int) *big.Int {
		return x.Mul(x, y)
	})
}
