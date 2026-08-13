package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return MINMAX() },
		func() vm.OP { return QMIN() },
		func() vm.OP { return QMAX() },
		func() vm.OP { return QMINMAX() },
		func() vm.OP { return QABS() },
		func() vm.OP { return SGN() },
		func() vm.OP { return CMP() },
		func() vm.OP { return ISNAN() },
		func() vm.OP { return CHKNAN() },
		func() vm.OP { return QSGN() },
		func() vm.OP { return QLESS() },
		func() vm.OP { return QEQUAL() },
		func() vm.OP { return QLEQ() },
		func() vm.OP { return QGREATER() },
		func() vm.OP { return QNEQ() },
		func() vm.OP { return QGEQ() },
		func() vm.OP { return QCMP() },
	)
	vm.ArgList = append(vm.ArgList, qEqIntOp, qLessIntOp, qGtIntOp, qNeqIntOp)
}

func minMaxOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			x, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			y, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}

			if x == nil {
				y = nil
			} else if y == nil {
				x = nil
			} else if x.Cmp(y) > 0 {
				x, y = y, x
			}

			if mode&2 != 0 {
				if err = pushMaybeInt(state, x, quiet); err != nil {
					return err
				}
			}
			if mode&4 != 0 {
				return pushMaybeInt(state, y, quiet)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func absOp(name string, prefix helpers.BitPrefix, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := popIntOperand(state, quiet)
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}
			if x.Sign() < 0 {
				x = x.Neg(x)
			}
			return pushMaybeInt(state, x, quiet)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func compareOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			x, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			if x == nil || y == nil {
				return pushNaNOrOverflow(state, quiet)
			}
			return pushSmallInt(state, compareModeValue(mode, x.Cmp(y)))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func compareIntOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(prefix),
		ArgBits:  8,
		Action: func(state *vm.State, args uint64) error {
			x, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}
			return pushSmallInt(state, compareModeValue(mode, compareBigIntInt64(x, int64(int8(args)))))
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

func signOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}
			return pushSmallInt(state, compareModeValue(mode, x.Sign()))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func MINMAX() *helpers.SimpleOP {
	return minMaxOp("MINMAX", helpers.BytesPrefix(0xB6, 0x0A), 6, false)
}

func QMIN() *helpers.SimpleOP {
	return minMaxOp("QMIN", helpers.BytesPrefix(0xB7, 0xB6, 0x08), 3, true)
}

func QMAX() *helpers.SimpleOP {
	return minMaxOp("QMAX", helpers.BytesPrefix(0xB7, 0xB6, 0x09), 5, true)
}

func QMINMAX() *helpers.SimpleOP {
	return minMaxOp("QMINMAX", helpers.BytesPrefix(0xB7, 0xB6, 0x0A), 7, true)
}

func QABS() *helpers.SimpleOP {
	return absOp("QABS", helpers.BytesPrefix(0xB7, 0xB6, 0x0B), true)
}

func SGN() *helpers.SimpleOP {
	return signOp("SGN", helpers.BytesPrefix(0xB8), 0x987, false)
}

func CMP() *helpers.SimpleOP {
	return compareOp("CMP", helpers.BytesPrefix(0xBF), 0x987, false)
}

func ISNAN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			return state.Stack.PushBool(x == nil)
		},
		Name:      "ISNAN",
		BitPrefix: helpers.BytesPrefix(0xC4),
	}
}

func CHKNAN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			return pushMaybeInt(state, x, false)
		},
		Name:      "CHKNAN",
		BitPrefix: helpers.BytesPrefix(0xC5),
	}
}

func QSGN() *helpers.SimpleOP {
	return signOp("QSGN", helpers.BytesPrefix(0xB7, 0xB8), 0x987, true)
}

func QLESS() *helpers.SimpleOP {
	return compareOp("QLESS", helpers.BytesPrefix(0xB7, 0xB9), 0x887, true)
}

func QEQUAL() *helpers.SimpleOP {
	return compareOp("QEQUAL", helpers.BytesPrefix(0xB7, 0xBA), 0x878, true)
}

func QLEQ() *helpers.SimpleOP {
	return compareOp("QLEQ", helpers.BytesPrefix(0xB7, 0xBB), 0x877, true)
}

func QGREATER() *helpers.SimpleOP {
	return compareOp("QGREATER", helpers.BytesPrefix(0xB7, 0xBC), 0x788, true)
}

func QNEQ() *helpers.SimpleOP {
	return compareOp("QNEQ", helpers.BytesPrefix(0xB7, 0xBD), 0x787, true)
}

func QGEQ() *helpers.SimpleOP {
	return compareOp("QGEQ", helpers.BytesPrefix(0xB7, 0xBE), 0x778, true)
}

func QCMP() *helpers.SimpleOP {
	return compareOp("QCMP", helpers.BytesPrefix(0xB7, 0xBF), 0x987, true)
}

var (
	qEqIntOp   = compareIntOp("QEQINT", helpers.BytesPrefix(0xB7, 0xC0), 0x878, true)
	qLessIntOp = compareIntOp("QLESSINT", helpers.BytesPrefix(0xB7, 0xC1), 0x887, true)
	qGtIntOp   = compareIntOp("QGTINT", helpers.BytesPrefix(0xB7, 0xC2), 0x788, true)
	qNeqIntOp  = compareIntOp("QNEQINT", helpers.BytesPrefix(0xB7, 0xC3), 0x787, true)
)

func QEQINT(value int8) vm.OP {
	return vm.Bind(qEqIntOp, uint64(uint8(value)))
}

func QLESSINT(value int8) vm.OP {
	return vm.Bind(qLessIntOp, uint64(uint8(value)))
}

func QGTINT(value int8) vm.OP {
	return vm.Bind(qGtIntOp, uint64(uint8(value)))
}

func QNEQINT(value int8) vm.OP {
	return vm.Bind(qNeqIntOp, uint64(uint8(value)))
}
