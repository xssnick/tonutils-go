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
		func() vm.OP { return QEQINT(0) },
		func() vm.OP { return QLESSINT(0) },
		func() vm.OP { return QGTINT(0) },
		func() vm.OP { return QNEQINT(0) },
	)
}

func minMaxOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			y, err := state.Stack.PopInt()
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
			x, err := state.Stack.PopInt()
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
			y, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
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

func compareIntOp(name string, prefix helpers.BitPrefix, mode int, value int8, quiet bool) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}
			return pushSmallInt(state, compareModeValue(mode, x.Cmp(big.NewInt(int64(value)))))
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

func signOp(name string, prefix helpers.BitPrefix, mode int, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
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
			x, err := state.Stack.PopInt()
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
			x, err := state.Stack.PopInt()
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

func QEQINT(value int8) *helpers.AdvancedOP {
	return compareIntOp("QEQINT", helpers.BytesPrefix(0xB7, 0xC0), 0x878, value, true)
}

func QLESSINT(value int8) *helpers.AdvancedOP {
	return compareIntOp("QLESSINT", helpers.BytesPrefix(0xB7, 0xC1), 0x887, value, true)
}

func QGTINT(value int8) *helpers.AdvancedOP {
	return compareIntOp("QGTINT", helpers.BytesPrefix(0xB7, 0xC2), 0x788, value, true)
}

func QNEQINT(value int8) *helpers.AdvancedOP {
	return compareIntOp("QNEQINT", helpers.BytesPrefix(0xB7, 0xC3), 0x787, value, true)
}
