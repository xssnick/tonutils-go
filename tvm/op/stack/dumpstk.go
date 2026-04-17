package stack

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return DUMPSTK() },
		func() vm.OP { return DUMP(0) },
		func() vm.OP { return DEBUG(1) },
		func() vm.OP { return DEBUGSTR(nil) },
		func() vm.OP { return STRDUMP() },
	)
}

func DUMPSTK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			vm.Tracef("#DEBUG#: stack(%d values)\n%s", state.Stack.Len(), state.Stack.String())
			return nil
		},
		Name:      "DUMPSTK",
		BitPrefix: helpers.BytesPrefix(0xFE, 0x00),
	}
}

func DUMP(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if int(idx) >= state.Stack.Len() {
				vm.Tracef("#DEBUG#: s%d is absent", idx)
				return nil
			}

			val, err := state.Stack.Get(int(idx))
			if err != nil {
				return nil
			}

			vm.Tracef("#DEBUG#: s%d = %s", idx, debugValueString(val))
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("DUMP s%d", idx)
		},
		BitPrefix:     helpers.UIntPrefix(0xFE2, 12),
		FixedSizeBits: 4,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			idx = uint8(v)
			return nil
		},
	}
}

func DEBUG(arg uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			vm.Tracef("DEBUG %d", arg)
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("DEBUG %d", arg)
		},
		BitPrefix:     helpers.BytesPrefix(0xFE),
		FixedSizeBits: 8,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(arg), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			arg = uint8(v)
			return nil
		},
	}
}

func STRDUMP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() == 0 {
				vm.Tracef("#DEBUG#: s0 is absent")
				return nil
			}

			val, err := state.Stack.Get(0)
			if err != nil {
				return nil
			}

			sl, ok := val.(*cell.Slice)
			if !ok {
				vm.Tracef("#DEBUG#: is not a slice")
				return nil
			}

			if sl.BitsLeft()%8 != 0 {
				vm.Tracef("#DEBUG#: slice contains not valid bits count")
				return nil
			}

			cp := sl.Copy()
			data, err := cp.LoadSlice(cp.BitsLeft())
			if err != nil {
				vm.Tracef("#DEBUG#: failed to load slice")
				return nil
			}

			vm.Tracef("#DEBUG#: %s", string(data))
			return nil
		},
		Name:      "STRDUMP",
		BitPrefix: helpers.BytesPrefix(0xFE, 0x14),
	}
}

type debugStrOp struct {
	data []byte
}

func DEBUGSTR(data []byte) vm.OP {
	if len(data) == 0 {
		data = []byte{0}
	} else {
		data = append([]byte{}, data...)
	}
	if len(data) > 16 {
		data = data[:16]
	}
	return &debugStrOp{data: data}
}

func (op *debugStrOp) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(helpers.UIntPrefix(0xFEF, 12))
}

func (op *debugStrOp) Deserialize(code *cell.Slice) error {
	return op.DeserializeMatched(code)
}

func (op *debugStrOp) DeserializeMatched(code *cell.Slice) error {
	if _, err := code.LoadSlice(12); err != nil {
		return err
	}
	v, err := code.LoadUInt(4)
	if err != nil {
		return err
	}
	data, err := code.LoadSlice(uint(v+1) * 8)
	if err != nil {
		return err
	}
	op.data = append(op.data[:0], data...)
	return nil
}

func (op *debugStrOp) Serialize() *cell.Builder {
	data := op.data
	if len(data) == 0 {
		data = []byte{0}
	}
	if len(data) > 16 {
		data = data[:16]
	}
	return cell.BeginCell().
		MustStoreUInt(0xFEF, 12).
		MustStoreUInt(uint64(len(data)-1), 4).
		MustStoreSlice(data, uint(len(data))*8)
}

func (op *debugStrOp) SerializeText() string {
	return fmt.Sprintf("DEBUGSTR %X", op.data)
}

func (op *debugStrOp) Interpret(state *vm.State) error {
	vm.Tracef("DEBUGSTR %X", op.data)
	return nil
}

func (op *debugStrOp) InstructionBits() int64 {
	return 16 + int64(len(op.data))*8
}

func debugValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil [nil]"
	case vm.NaN, *vm.NaN:
		return "NaN [nan]"
	case *big.Int:
		return x.String() + " [int]"
	case *cell.Slice:
		return x.WithoutObserver().MustToCell().Dump() + " [slice]"
	case *cell.Builder:
		return x.WithoutObserver().EndCell().Dump() + " [builder]"
	case *cell.Cell:
		return x.Dump() + " [cell]"
	default:
		return fmt.Sprintf("%v [%T]", x, x)
	}
}
