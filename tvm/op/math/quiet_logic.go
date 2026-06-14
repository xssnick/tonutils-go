package math

import (
	"fmt"
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
		func() vm.OP { return QLSHIFTCODE(0) },
		func() vm.OP { return QRSHIFTCODE(0) },
		func() vm.OP { return QPOW2() },
	)
}

func quietBinaryLogicOp(name string, prefix helpers.BitPrefix, fn func(x, y *big.Int) *big.Int) *helpers.SimpleOP {
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
			return state.Stack.PushIntQuiet(fn(x, y))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietShiftOp(name string, prefix helpers.BitPrefix, right bool) *helpers.SimpleOP {
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
			if x == nil || y == nil || y.Sign() < 0 || y.Cmp(bigIntMaxShift) > 0 {
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

func quietShiftCodeOp(name string, prefix helpers.BitPrefix, value int8, right bool) *helpers.AdvancedOP {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
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
				if right && state.GlobalVersion < 14 {
					return pushSmallInt(state, 0)
				}
				if !right && state.GlobalVersion < 13 {
					return pushSmallInt(state, 0)
				}
				return pushNaNOrOverflow(state, true)
			}

			res := new(big.Int).Set(x)
			if right {
				res.Rsh(res, uint(imm()))
			} else {
				res.Lsh(res, uint(imm()))
			}
			return state.Stack.PushIntQuiet(res)
		},
		BitPrefix:       prefix,
		SerializeSuffix: serializeImmediate,
		NameSerializer: func() string {
			return fmt.Sprintf("%d %s", imm(), name)
		},
		DeserializeSuffix: deserializeImmediate,
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

func QLSHIFTCODE(value int8) *helpers.AdvancedOP {
	return quietShiftCodeOp("QLSHIFT#", helpers.BytesPrefix(0xB7, 0xAA), value, false)
}

func QRSHIFTCODE(value int8) *helpers.AdvancedOP {
	return quietShiftCodeOp("QRSHIFT#", helpers.BytesPrefix(0xB7, 0xAB), value, true)
}

func QPOW2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 1); err != nil {
				return err
			}
			y, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if y == nil || y.Sign() < 0 || y.Cmp(bigIntMaxShift) > 0 {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushIntQuiet(new(big.Int).Lsh(bigIntOne, uint(y.Uint64())))
		},
		Name:      "QPOW2",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xAE),
	}
}
