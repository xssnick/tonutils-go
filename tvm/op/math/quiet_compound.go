package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return qDivModFamily(0) },
		func() vm.OP { return qShrModFamily(0) },
		func() vm.OP { return qMulDivModFamily(0) },
		func() vm.OP { return qMulShrModFamily(0) },
		func() vm.OP { return qShlDivModFamily(0) },
	)

	// B7A93*, B7A9B*, and B7A9D* quiet immediate variants are listed in
	// docs.ton.org, but upstream C++ node keeps their registrations commented out
	// in crypto/vm/arithops.cpp. Keep them unregistered here until cppnode enables
	// them too.
}

func qFamilySuffix(args uint8) (string, error) {
	switch args & 3 {
	case 0:
		return "", nil
	case 1:
		return "R", nil
	case 2:
		return "C", nil
	default:
		return "", vmerr.Error(vmerr.CodeInvalidOpcode)
	}
}

func qRoundDiv(x, y *big.Int, args uint8) (*big.Int, *big.Int, error) {
	if y.Sign() == 0 {
		return nil, nil, nil
	}

	switch args & 3 {
	case 0:
		q, r := helpers.DivFloor(new(big.Int).Set(x), new(big.Int).Set(y))
		return q, r, nil
	case 1:
		q := helpers.DivRound(new(big.Int).Set(x), new(big.Int).Set(y))
		r := new(big.Int).Sub(new(big.Int).Set(x), new(big.Int).Mul(new(big.Int).Set(y), q))
		return q, r, nil
	case 2:
		q := helpers.DivCeil(new(big.Int).Set(x), new(big.Int).Set(y))
		r := new(big.Int).Sub(new(big.Int).Set(x), new(big.Int).Mul(new(big.Int).Set(y), q))
		return q, r, nil
	default:
		return nil, nil, vmerr.Error(vmerr.CodeInvalidOpcode)
	}
}

func qPushNaNs(state *vm.State, count int) error {
	for i := 0; i < count; i++ {
		if err := state.Stack.PushAny(vm.NaN{}); err != nil {
			return err
		}
	}
	return nil
}

func qPushResult(state *vm.State, val *big.Int) error {
	if val == nil {
		return state.Stack.PushAny(vm.NaN{})
	}
	return state.Stack.PushOwnedIntQuiet(val)
}

func qPushSelected(state *vm.State, d uint8, q, r *big.Int) error {
	switch d {
	case 1:
		return qPushResult(state, q)
	case 2:
		return qPushResult(state, r)
	case 3:
		if err := qPushResult(state, q); err != nil {
			return err
		}
		return qPushResult(state, r)
	default:
		return vmerr.Error(vmerr.CodeInvalidOpcode)
	}
}

func qInvalidResultCount(d uint8) int {
	if d == 3 {
		return 2
	}
	return 1
}

func qShift256Value(x *big.Int) (uint, bool) {
	if x == nil || !x.IsInt64() {
		return 0, false
	}
	v := x.Int64()
	if v < 0 || v > 256 {
		return 0, false
	}
	return uint(v), true
}

func qCompoundOP(name func(uint8) string, prefix helpers.BitPrefix, args uint8, action func(*vm.State, uint8) error) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			return action(state, args)
		},
		NameSerializer: func() string {
			return name(args)
		},
		BitPrefix: prefix,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(args), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			args = uint8(val)
			return nil
		},
	}
}

func qDivModName(args uint8) string {
	suffix, err := qFamilySuffix(args)
	if err != nil {
		return "QDIVMOD<invalid>"
	}

	d := (args >> 2) & 3
	name := "Q"
	if d == 0 {
		name += "ADD"
		d = 3
	}
	if d&1 != 0 {
		name += "DIV"
	}
	if d&2 != 0 {
		name += "MOD"
	}
	return name + suffix
}

func qDivModFamily(args uint8) *helpers.AdvancedOP {
	return qCompoundOP(qDivModName, helpers.UIntPrefix(0xB7A90, 20), args, func(state *vm.State, args uint8) error {
		if _, err := qFamilySuffix(args); err != nil {
			return err
		}

		d := (args >> 2) & 3
		add := d == 0
		if add {
			d = 3
		}
		required := 2
		if add {
			required = 3
		}
		if err := checkStackDepth(state, required); err != nil {
			return err
		}

		y, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		var w *big.Int
		if add {
			w, err = state.Stack.PopInt()
			if err != nil {
				return err
			}
		}
		x, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if x == nil || y == nil || (add && w == nil) || y.Sign() == 0 {
			return qPushNaNs(state, qInvalidResultCount(d))
		}
		dividend := new(big.Int).Set(x)
		if add {
			dividend.Add(dividend, w)
		}
		q, r, err := qRoundDiv(dividend, y, args)
		if err != nil {
			return err
		}
		return qPushSelected(state, d, q, r)
	})
}

func qShrModName(args uint8) string {
	suffix, err := qFamilySuffix(args)
	if err != nil {
		return "QSHRMOD<invalid>"
	}

	switch (args >> 2) & 3 {
	case 0:
		return "QADDRSHIFTMOD" + suffix
	case 1:
		return "QRSHIFT" + suffix
	case 2:
		return "QMODPOW2" + suffix
	default:
		return "QRSHIFTMOD" + suffix
	}
}

func qShrModFamily(args uint8) *helpers.AdvancedOP {
	return qCompoundOP(qShrModName, helpers.UIntPrefix(0xB7A92, 20), args, func(state *vm.State, args uint8) error {
		if _, err := qFamilySuffix(args); err != nil {
			return err
		}

		d := (args >> 2) & 3
		add := d == 0
		if add {
			d = 3
		}
		required := 2
		if add {
			required = 3
		}
		if err := checkStackDepth(state, required); err != nil {
			return err
		}

		shift, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		shiftValue, shiftValid := qShift256Value(shift)
		if !shiftValid && state.GlobalVersion < 14 {
			return vmerr.Error(vmerr.CodeRangeCheck)
		}
		var w *big.Int
		if add {
			w, err = state.Stack.PopInt()
			if err != nil {
				return err
			}
		}
		x, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if x == nil || (add && w == nil) || !shiftValid {
			return qPushNaNs(state, qInvalidResultCount(d))
		}
		dividend := new(big.Int).Set(x)
		if add {
			dividend.Add(dividend, w)
		}
		divider := new(big.Int).Lsh(bigIntOne, shiftValue)
		q, r, err := qRoundDiv(dividend, divider, args)
		if err != nil {
			return err
		}
		return qPushSelected(state, d, q, r)
	})
}

func qMulDivModName(args uint8) string {
	suffix, err := qFamilySuffix(args)
	if err != nil {
		return "QMULDIVMOD<invalid>"
	}

	d := (args >> 2) & 3
	name := "QMUL"
	if d == 0 {
		name += "ADD"
		d = 3
	}
	if d&1 != 0 {
		name += "DIV"
	}
	if d&2 != 0 {
		name += "MOD"
	}
	return name + suffix
}

func qMulDivModFamily(args uint8) *helpers.AdvancedOP {
	return qCompoundOP(qMulDivModName, helpers.UIntPrefix(0xB7A98, 20), args, func(state *vm.State, args uint8) error {
		if _, err := qFamilySuffix(args); err != nil {
			return err
		}

		d := (args >> 2) & 3
		add := d == 0
		if add {
			d = 3
		}
		required := 3
		if add {
			required = 4
		}
		if err := checkStackDepth(state, required); err != nil {
			return err
		}

		z, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		var w *big.Int
		if add {
			w, err = state.Stack.PopInt()
			if err != nil {
				return err
			}
		}
		y, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		x, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if x == nil || y == nil || z == nil || (add && w == nil) || z.Sign() == 0 {
			return qPushNaNs(state, qInvalidResultCount(d))
		}
		dividend := new(big.Int).Mul(new(big.Int).Set(x), y)
		if add {
			dividend.Add(dividend, w)
		}
		q, r, err := qRoundDiv(dividend, z, args)
		if err != nil {
			return err
		}
		return qPushSelected(state, d, q, r)
	})
}

func qMulShrModName(args uint8) string {
	suffix, err := qFamilySuffix(args)
	if err != nil {
		return "QMULSHRMOD<invalid>"
	}

	switch (args >> 2) & 3 {
	case 0:
		return "QMULADDRSHIFTMOD" + suffix
	case 1:
		return "QMULRSHIFT" + suffix
	case 2:
		return "QMULMODPOW2" + suffix
	default:
		return "QMULRSHIFTMOD" + suffix
	}
}

func qMulShrModFamily(args uint8) *helpers.AdvancedOP {
	return qCompoundOP(qMulShrModName, helpers.UIntPrefix(0xB7A9A, 20), args, func(state *vm.State, args uint8) error {
		if _, err := qFamilySuffix(args); err != nil {
			return err
		}

		d := (args >> 2) & 3
		add := d == 0
		if add {
			d = 3
		}
		required := 3
		if add {
			required = 4
		}
		if err := checkStackDepth(state, required); err != nil {
			return err
		}

		shift, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		var w *big.Int
		if add {
			w, err = state.Stack.PopInt()
			if err != nil {
				return err
			}
		}
		y, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		x, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if shift == nil || shift.Sign() < 0 || shift.Cmp(bigIntMaxCompoundShift) > 0 ||
			x == nil || y == nil || (add && w == nil) {
			return qPushNaNs(state, qInvalidResultCount(d))
		}
		dividend := new(big.Int).Mul(new(big.Int).Set(x), y)
		if add {
			dividend.Add(dividend, w)
		}
		divider := new(big.Int).Lsh(bigIntOne, uint(shift.Uint64()))
		q, r, err := qRoundDiv(dividend, divider, args)
		if err != nil {
			return err
		}
		return qPushSelected(state, d, q, r)
	})
}

func qShlDivModName(args uint8) string {
	suffix, err := qFamilySuffix(args)
	if err != nil {
		return "QLSHIFTDIVMOD<invalid>"
	}

	switch (args >> 2) & 3 {
	case 0:
		return "QLSHIFTADDDIVMOD" + suffix
	case 1:
		return "QLSHIFTDIV" + suffix
	case 2:
		return "QLSHIFTMOD" + suffix
	default:
		return "QLSHIFTDIVMOD" + suffix
	}
}

func qShlDivModFamily(args uint8) *helpers.AdvancedOP {
	return qCompoundOP(qShlDivModName, helpers.UIntPrefix(0xB7A9C, 20), args, func(state *vm.State, args uint8) error {
		if _, err := qFamilySuffix(args); err != nil {
			return err
		}

		d := (args >> 2) & 3
		add := d == 0
		if add {
			d = 3
		}
		required := 3
		if add {
			required = 4
		}
		if err := checkStackDepth(state, required); err != nil {
			return err
		}

		shift, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		z, err := state.Stack.PopInt()
		if err != nil {
			return err
		}
		var w *big.Int
		if add {
			w, err = state.Stack.PopInt()
			if err != nil {
				return err
			}
		}
		x, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if shift == nil || shift.Sign() < 0 || shift.Cmp(bigIntMaxCompoundShift) > 0 ||
			x == nil || z == nil || (add && w == nil) || z.Sign() == 0 {
			return qPushNaNs(state, qInvalidResultCount(d))
		}
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		if add {
			dividend.Add(dividend, w)
		}
		q, r, err := qRoundDiv(dividend, z, args)
		if err != nil {
			return err
		}
		return qPushSelected(state, d, q, r)
	})
}
