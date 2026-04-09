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
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushIntQuiet(fn(x))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietBinaryIntOp(name string, prefix helpers.BitPrefix, fn func(x, y *big.Int) *big.Int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
			return state.Stack.PushIntQuiet(fn(x, y))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietTinyIntOp(name string, prefix helpers.BitPrefix, value int8, fn func(x *big.Int, arg int64) *big.Int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushIntQuiet(fn(x, int64(value)))
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
		return x.Add(x, big.NewInt(1))
	})
}

func QDEC() *helpers.SimpleOP {
	return quietUnaryIntOp("QDEC", helpers.BytesPrefix(0xB7, 0xA5), func(x *big.Int) *big.Int {
		return x.Sub(x, big.NewInt(1))
	})
}

func QADDINT(value int8) *helpers.AdvancedOP {
	return quietTinyIntOp("QADDINT", helpers.BytesPrefix(0xB7, 0xA6), value, func(x *big.Int, arg int64) *big.Int {
		return x.Add(x, big.NewInt(arg))
	})
}

func QMULINT(value int8) *helpers.AdvancedOP {
	return quietTinyIntOp("QMULINT", helpers.BytesPrefix(0xB7, 0xA7), value, func(x *big.Int, arg int64) *big.Int {
		return x.Mul(x, big.NewInt(arg))
	})
}

func QMUL() *helpers.SimpleOP {
	return quietBinaryIntOp("QMUL", helpers.BytesPrefix(0xB7, 0xA8), func(x, y *big.Int) *big.Int {
		return x.Mul(x, y)
	})
}
