package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return newIfBitJmpRefOp(0, false, nil) },
	)
	vm.ArgList = append(vm.ArgList, ifBitJmpOp)
}

// ifBitJmpArgs is the 6-bit operand as it sits on the wire: the bit index in
// the low five bits, the negation flag on top. An index that does not fit five
// bits would run into the flag and assemble a different instruction, so it is
// refused rather than truncated. Decoding cannot reach that: the field it reads
// is five bits wide.
func ifBitJmpArgs(bit uint8, negate bool) uint64 {
	if bit > 0x1F {
		panic(fmt.Sprintf("exec: bit index %d does not fit the instruction's 5-bit field", bit))
	}
	args := uint64(bit)
	if negate {
		args |= 0x20
	}
	return args
}

func IFBITJMP(bit uint8) vm.OP {
	return vm.Bind(ifBitJmpOp, ifBitJmpArgs(bit, false))
}

func IFNBITJMP(bit uint8) vm.OP {
	return vm.Bind(ifBitJmpOp, ifBitJmpArgs(bit, true))
}

func IFBITJMPREF(bit uint8, ref *cell.Cell) vm.OP {
	return bindRefCodeOp(newIfBitJmpRefOp(bit, false, nil), ref)
}

func IFNBITJMPREF(bit uint8, ref *cell.Cell) vm.OP {
	return bindRefCodeOp(newIfBitJmpRefOp(bit, true, nil), ref)
}

var ifBitJmpOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(10, []byte{0xE3, 0x80})),
	ArgBits:  6,
	Action: func(state *vm.State, args uint64) error {
		if err := checkStackDepth(state, 2); err != nil {
			return err
		}

		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}
		x, err := state.Stack.PopIntFinite()
		if err != nil {
			return err
		}
		val := x.Bit(int(args&0x1F)) != 0
		if err = state.Stack.PushOwnedInt(x); err != nil {
			return err
		}
		if val != (args&0x20 != 0) {
			return state.Jump(cont)
		}
		return nil
	},
	Name: func(args uint64) string {
		if args&0x20 != 0 {
			return fmt.Sprintf("IFNBITJMP %d", args&0x1F)
		}
		return fmt.Sprintf("IFBITJMP %d", args&0x1F)
	},
})

func newIfBitJmpRefOp(bit uint8, negate bool, ref *cell.Cell) *refCodeOp {
	op := newRefCodeOp("", helpers.SlicePrefix(10, []byte{0xE3, 0xC0}), 1, func(state *vm.State, refs []*cell.Cell, traces []*cell.Trace) error {
		x, err := state.Stack.PopIntFinite()
		if err != nil {
			return err
		}
		val := x.Bit(int(bit)) != 0
		if err = state.Stack.PushOwnedInt(x); err != nil {
			return err
		}
		if val != negate {
			return jumpToCodeCell(state, refs[0], traces[0])
		}
		return nil
	})
	op.name = "IFBITJMPREF"
	op.fixedBits = 6
	op.serializeSuffix = func() *cell.Builder {
		raw := uint64(bit & 0x1F)
		if negate {
			raw |= 0x20
		}
		return cell.BeginCell().MustStoreUInt(raw, 6)
	}
	op.deserializeSuffix = func(code *cell.Slice) error {
		raw, err := code.LoadUInt(6)
		if err != nil {
			return err
		}
		bit = uint8(raw & 0x1F)
		negate = raw&0x20 != 0
		if negate {
			op.name = "IFNBITJMPREF"
		} else {
			op.name = "IFBITJMPREF"
		}
		return nil
	}
	if negate {
		op.name = "IFNBITJMPREF"
	}
	if ref != nil {
		op.refs[0] = ref
		op.refTraces[0] = ref.Trace()
	}
	return op
}
