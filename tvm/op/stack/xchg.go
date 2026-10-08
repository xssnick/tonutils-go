package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, xchgOp)
}

var xchgPrefixed = helpers.NewPrefixed(
	helpers.UIntPrefix(0x0, 4),
	helpers.UIntPrefix(0x1, 4),
	helpers.UIntPrefix(0x10, 8),
)

// Besides the two indices the operand carries which encoding the instruction
// used: only the 16-bit long form rejects arguments the short forms accept, and
// only it charges 16 bits when both indices are small.
const xchgLongForm = 1 << 16

func packXchgArgs(a, b uint8, long bool) uint64 {
	args := uint64(a)<<8 | uint64(b)
	if long {
		args |= xchgLongForm
	}
	return args
}

func unpackXchgArgs(args uint64) (a, b uint8, long bool) {
	return uint8(args >> 8), uint8(args), args&xchgLongForm != 0
}

var xchgOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: xchgPrefixed,
	Action: func(state *vm.State, args uint64) error {
		a, b, long := unpackXchgArgs(args)
		if long && (a == 0 || a >= b) {
			return vmerr.Error(vmerr.CodeInvalidOpcode, "invalid XCHG arguments")
		}
		return state.Stack.Exchange(int(a), int(b))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		a, err := code.LoadUInt(4)
		if err != nil {
			return 0, err
		}

		b, err := code.LoadUInt(4)
		if err != nil {
			// consensus-critical: a truncated opcode is zero-padded and charged by
			// the padded opcode-table entry; a zero-padded second nibble after s1
			// selects the 16-bit long form
			if a == 1 && helpers.PeekZeroPaddedOpcode(code, 4) == 0 {
				return packXchgArgs(0, 0, true), err
			}
			return 0, err
		}

		if a == 1 && b == 0 {
			a, err = code.LoadUInt(4)
			if err != nil {
				return packXchgArgs(1, 0, true), err
			}

			b, err = code.LoadUInt(4)
			if err != nil {
				return packXchgArgs(1, 0, true), err
			}

			return packXchgArgs(uint8(a), uint8(b), true), nil
		}

		return packXchgArgs(uint8(a), uint8(b), false), nil
	},
	Bits: func(args uint64) int64 {
		a, b, long := unpackXchgArgs(args)
		if long {
			return 16
		}
		if a == 0 || b == 0 || a == 1 || b == 1 {
			return 8
		}
		return 16
	},
	Serializer: func(args uint64) *cell.Builder {
		a, b, _ := unpackXchgArgs(args)
		if a == 0 || b == 0 {
			with := a
			if with == 0 {
				with = b
			}
			return helpers.Builder([]byte{0x00 | with})
		}
		if a == 1 || b == 1 {
			with := a
			if with == 1 {
				with = b
			}
			return helpers.Builder([]byte{0x10 | with})
		}
		return helpers.Builder([]byte{0x10, (a << 4) | b})
	},
	Name: func(args uint64) string {
		a, b, _ := unpackXchgArgs(args)
		return fmt.Sprintf("s%d,s%d XCHG", a, b)
	},
})

func XCHG(a, b uint8) vm.OP {
	return vm.Bind(xchgOp, packXchgArgs(a, b, a > 1 && b > 1))
}
