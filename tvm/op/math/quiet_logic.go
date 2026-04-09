package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return QAND() },
		func() vm.OP { return QOR() },
		func() vm.OP { return QXOR() },
		func() vm.OP { return QLSHIFT() },
		func() vm.OP { return QRSHIFT() },
		func() vm.OP { return QPOW2() },
	)
}

func quietBinaryLogicOp(name string, prefix helpers.BitPrefix, fn func(x, y *big.Int) *big.Int) *helpers.SimpleOP {
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

func quietShiftOp(name string, prefix helpers.BitPrefix, right bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, true)
			}

			res := new(big.Int).Set(x)
			if right {
				res.Rsh(res, uint(y.Uint64()))
			} else {
				res.Lsh(res, uint(y.Uint64()))
			}
			return state.Stack.PushIntQuiet(res)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func QAND() *helpers.SimpleOP {
	return quietBinaryLogicOp("QAND", helpers.BytesPrefix(0xB7, 0xB0), func(x, y *big.Int) *big.Int {
		return x.And(x, y)
	})
}

func QOR() *helpers.SimpleOP {
	return quietBinaryLogicOp("QOR", helpers.BytesPrefix(0xB7, 0xB1), func(x, y *big.Int) *big.Int {
		return x.Or(x, y)
	})
}

func QXOR() *helpers.SimpleOP {
	return quietBinaryLogicOp("QXOR", helpers.BytesPrefix(0xB7, 0xB2), func(x, y *big.Int) *big.Int {
		return x.Xor(x, y)
	})
}

func QLSHIFT() *helpers.SimpleOP {
	return quietShiftOp("QLSHIFT", helpers.BytesPrefix(0xB7, 0xAC), false)
}

func QRSHIFT() *helpers.SimpleOP {
	return quietShiftOp("QRSHIFT", helpers.BytesPrefix(0xB7, 0xAD), true)
}

func QPOW2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			return state.Stack.PushIntQuiet(new(big.Int).Lsh(big.NewInt(1), uint(y.Uint64())))
		},
		Name:      "QPOW2",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xAE),
	}
}
