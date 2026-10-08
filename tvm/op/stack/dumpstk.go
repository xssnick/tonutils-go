package stack

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return DUMPSTK() },
		func() vm.OP { return DEBUGSTR(nil) },
		func() vm.OP { return STRDUMP() },
	)
	vm.ArgList = append(vm.ArgList, dumpOp, debugOp)
}

func DUMPSTK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if !state.TraceEnabled() {
				return nil
			}

			state.Trace(debugStackString(state.Stack))
			return nil
		},
		Name:      "DUMPSTK",
		BitPrefix: helpers.BytesPrefix(0xFE, 0x00),
	}
}

var dumpOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xFE2, 12)),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		if !state.TraceEnabled() {
			return nil
		}

		if int(args) >= state.Stack.Len() {
			state.Tracef("#DEBUG#: s%d is absent", args)
			return nil
		}

		val, err := state.Stack.Get(int(args))
		if err != nil {
			return nil
		}

		state.Tracef("#DEBUG#: s%d = %s", args, debugValueString(val))
		return nil
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("DUMP s%d", args)
	},
})

func DUMP(idx uint8) vm.OP {
	return vm.Bind(dumpOp, uint64(idx))
}

var debugOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xFE)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		if !state.TraceEnabled() {
			return nil
		}

		state.Tracef("DEBUG %d", args)
		return nil
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("DEBUG %d", args)
	},
})

func DEBUG(arg uint8) vm.OP {
	return vm.Bind(debugOp, uint64(arg))
}

func STRDUMP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if !state.TraceEnabled() {
				return nil
			}

			if state.Stack.Len() == 0 {
				state.Tracef("#DEBUG#: s0 is absent")
				return nil
			}

			val, err := state.Stack.Get(0)
			if err != nil {
				return nil
			}

			sl, ok := val.(*cell.Slice)
			if !ok || sl == nil {
				state.Tracef("#DEBUG#: is not a slice")
				return nil
			}

			if sl.BitsLeft()%8 != 0 {
				state.Tracef("#DEBUG#: slice contains not valid bits count")
				return nil
			}

			cp := sl.Copy()
			data, err := cp.LoadSlice(cp.BitsLeft())
			if err != nil {
				state.Tracef("#DEBUG#: failed to load slice")
				return nil
			}

			state.Tracef("#DEBUG#: %s", string(data))
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
	if err := code.SkipBits(12); err != nil {
		return err
	}
	v, err := code.LoadUInt(4)
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	data, err := code.LoadSlice(uint(v+1) * 8)
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
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
	if !state.TraceEnabled() {
		return nil
	}

	state.Tracef("DEBUGSTR %X", op.data)
	return nil
}

func (op *debugStrOp) InstructionBits() int64 {
	return 16
}

func debugValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil [nil]"
	case vm.NaN:
		return "NaN [nan]"
	case *big.Int:
		return x.String() + " [int]"
	case *cell.Slice:
		if x == nil {
			return "null [slice]"
		}
		return x.WithoutTrace().MustToCell().Dump() + " [slice]"
	case *cell.Builder:
		if x == nil {
			return "null [builder]"
		}
		return x.WithoutTrace().EndCell().Dump() + " [builder]"
	case *cell.Cell:
		if x == nil {
			return "null [cell]"
		}
		return x.Dump() + " [cell]"
	default:
		return fmt.Sprintf("%v [%T]", x, x)
	}
}

func debugStackString(stack *vm.Stack) string {
	depth := stack.Len()
	dumpDepth := depth

	var b strings.Builder
	fmt.Fprintf(&b, "#DEBUG#: stack(%d values) : ", depth)
	if dumpDepth > 255 {
		b.WriteString("... ")
		dumpDepth = 255
	}
	for i := dumpDepth - 1; i >= 0; i-- {
		val, err := stack.Get(i)
		if err != nil {
			panic(err)
		}
		b.WriteString(debugValueString(val))
		b.WriteByte(' ')
	}

	return b.String()
}
