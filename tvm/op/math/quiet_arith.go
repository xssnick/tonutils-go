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
		func() vm.OP { return QADDINT(0) },
		func() vm.OP { return QMULINT(0) },
		func() vm.OP { return QMUL() },
	)
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

func quietTinyIntOp(name string, prefix helpers.BitPrefix, value int8, fn func(x, arg *big.Int) *big.Int) *helpers.AdvancedOP {
	arg := big.NewInt(int64(value))
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
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
			return state.Stack.PushOwnedIntQuiet(fn(x, arg))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%s %d", name, value)
		},
		BitPrefix: helpers.BytesPrefix(prefix.Data...),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreInt(int64(value), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadInt(8)
			if err != nil {
				return err
			}
			value = int8(v)
			arg.SetInt64(int64(value))
			return nil
		},
	}
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

func QADDINT(value int8) *helpers.AdvancedOP {
	return quietTinyIntOp("QADDINT", helpers.BytesPrefix(0xB7, 0xA6), value, func(x, arg *big.Int) *big.Int {
		return x.Add(x, arg)
	})
}

func QMULINT(value int8) *helpers.AdvancedOP {
	return quietTinyIntOp("QMULINT", helpers.BytesPrefix(0xB7, 0xA7), value, func(x, arg *big.Int) *big.Int {
		return x.Mul(x, arg)
	})
}

func QMUL() *helpers.SimpleOP {
	return quietBinaryIntOp("QMUL", helpers.BytesPrefix(0xB7, 0xA8), func(x, y *big.Int) *big.Int {
		return x.Mul(x, y)
	})
}
