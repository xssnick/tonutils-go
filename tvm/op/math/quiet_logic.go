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
		func() vm.OP { return QAND() },
		func() vm.OP { return QOR() },
		func() vm.OP { return QXOR() },
		func() vm.OP { return QLSHIFT() },
		func() vm.OP { return QRSHIFT() },
		func() vm.OP { return QLSHIFTCODE(1) },
		func() vm.OP { return QRSHIFTCODE(1) },
		func() vm.OP { return QPOW2() },
	)
}

func quietShiftOp(name string, prefix helpers.BitPrefix, right bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			shiftValid := y != nil && y.Sign() >= 0 && y.Cmp(bigIntMaxShift) <= 0
			if !shiftValid && state.GlobalVersion < 13 {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if !shiftValid {
				return pushNaNOrOverflow(state, true)
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResult(state.GlobalVersion, y.Uint64(), right), true)
			}

			var res *big.Int
			if right {
				res = new(big.Int).Rsh(x, uint(y.Uint64()))
			} else {
				res = leftShiftResult(x, y.Uint64())
			}
			return pushMaybeInt(state, res, true)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func quietShiftCodeOp(name string, prefix helpers.BitPrefix, value int, right bool) *helpers.AdvancedOP {
	imm, serializeImmediate, deserializeImmediate := newBytePlusOneImmediate(value)
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 1); err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResultThreshold(state.GlobalVersion, 14, uint64(imm()), right), true)
			}

			var res *big.Int
			if right {
				res = new(big.Int).Rsh(x, uint(imm()))
			} else {
				res = leftShiftResult(x, uint64(imm()))
			}
			return pushMaybeInt(state, res, true)
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
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			return pushMaybeInt(state, versionedAndResult(state.GlobalVersion, x, y), true)
		},
		Name:      "QAND",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xB0),
	}
}

func QOR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			return pushMaybeInt(state, versionedOrResult(state.GlobalVersion, x, y), true)
		},
		Name:      "QOR",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xB1),
	}
}

func QXOR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if x == nil || y == nil {
				return pushNaNOrOverflow(state, true)
			}

			// int64 fast path: XOR of two int64 values stays within int64,
			// operands (possibly shared statics) are never mutated.
			if x.IsInt64() && y.IsInt64() {
				return state.Stack.PushSmallInt(x.Int64() ^ y.Int64())
			}

			// XOR never leaves the 257-bit range, so the quiet push cannot NaN.
			return state.Stack.PushOwnedIntQuiet(new(big.Int).Xor(x, y))
		},
		Name:      "QXOR",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xB2),
	}
}

func QLSHIFT() *helpers.SimpleOP {
	return quietShiftOp("QLSHIFT", helpers.BytesPrefix(0xB7, 0xAC), false)
}

func QRSHIFT() *helpers.SimpleOP {
	return quietShiftOp("QRSHIFT", helpers.BytesPrefix(0xB7, 0xAD), true)
}

func QLSHIFTCODE(value int) *helpers.AdvancedOP {
	return quietShiftCodeOp("QLSHIFT#", helpers.BytesPrefix(0xB7, 0xAA), value, false)
}

func QRSHIFTCODE(value int) *helpers.AdvancedOP {
	return quietShiftCodeOp("QRSHIFT#", helpers.BytesPrefix(0xB7, 0xAB), value, true)
}

func QPOW2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 1); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if y == nil || y.Sign() < 0 || y.Cmp(bigIntMaxShift) > 0 {
				if state.GlobalVersion < 13 {
					return vmerr.Error(vmerr.CodeRangeCheck)
				}
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushIntQuiet(new(big.Int).Lsh(bigIntOne, uint(y.Uint64())))
		},
		Name:      "QPOW2",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xAE),
	}
}
