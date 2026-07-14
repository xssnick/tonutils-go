package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

type OpPUSHINT struct {
	helpers.Prefixed
	value           *big.Int
	instructionBits int64
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHINT(nil) })
}

var (
	pushIntPrefixed  = buildPushIntPrefixed()
	pushIntBigIntOne = big.NewInt(1)

	// pushIntSmallImmediates holds the tiny-form (0x70..0x7F) immediates -5..10,
	// indexed by the low 4 bits of the prefix. Deserialize reuses these instead of
	// allocating per decode; they must never be mutated. Interpret routes -5..10
	// through PushSmallInt, which pushes the vm package statics, so these values
	// never reach the stack.
	pushIntSmallImmediates = buildPushIntSmallImmediates()
)

func buildPushIntPrefixed() helpers.Prefixed {
	prefixes := []helpers.BitPrefix{
		helpers.UIntPrefix(0x7, 4),
		helpers.UIntPrefix(0x80, 8),
		helpers.UIntPrefix(0x81, 8),
	}
	for i := uint64(0); i < 31; i++ {
		prefixes = append(prefixes, helpers.UIntPrefix(0x82<<5|i, 13))
	}
	return helpers.NewPrefixed(prefixes...)
}

func buildPushIntSmallImmediates() [16]*big.Int {
	var vals [16]*big.Int
	for i := range vals {
		vals[i] = big.NewInt(int64(((i + 5) & 0xF) - 5))
	}
	return vals
}

func PUSHINT(value *big.Int) *OpPUSHINT {
	return &OpPUSHINT{
		Prefixed: pushIntPrefixed,
		value:    value,
	}
}

func (op *OpPUSHINT) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	switch prefix {
	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f:
		op.value = pushIntSmallImmediates[prefix&0xF]
		op.instructionBits = 8
		return nil
	case 0x80:
		op.instructionBits = 16
		val, err := code.LoadBigInt(8)
		if err != nil {
			return err
		}
		op.value = val
		return nil
	case 0x81:
		op.instructionBits = 24
		val, err := code.LoadBigInt(16)
		if err != nil {
			return err
		}
		op.value = val
		return nil
	case 0x82:
		op.instructionBits = 13
		szBytes, err := code.LoadUInt(5)
		if err != nil {
			return err
		}
		if szBytes == 31 {
			return vm.ErrCorruptedOpcode
		}

		val, err := loadPushIntValue(code, uint(szBytes*8+19))
		if err != nil {
			return err
		}

		op.value = val
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func loadPushIntValue(code *cell.Slice, sz uint) (*big.Int, error) {
	if sz <= 257 {
		return code.LoadBigInt(sz)
	}

	b, err := code.LoadSlice(sz)
	if err != nil {
		return nil, err
	}

	if offset := sz % 8; offset > 0 {
		for i := len(b) - 1; i >= 0; i-- {
			b[i] >>= 8 - offset
			if i > 0 {
				b[i] += b[i-1] << offset
			}
		}
	}

	val := new(big.Int).SetBytes(b)
	if val.Bit(int(sz-1)) == 0 {
		return val, nil
	}

	return val.Sub(val, new(big.Int).Lsh(pushIntBigIntOne, sz)), nil
}

func signedBigIntBits(value *big.Int) int {
	if value.Sign() >= 0 {
		return value.BitLen() + 1
	}

	absMinusOne := new(big.Int).Neg(value)
	absMinusOne.Sub(absMinusOne, pushIntBigIntOne)
	return absMinusOne.BitLen() + 1
}

func (op *OpPUSHINT) Serialize() *cell.Builder {
	bitsSz := signedBigIntBits(op.value)

	switch {
	case op.value.IsInt64() && op.value.Int64() >= -5 && op.value.Int64() <= 10:
		return cell.BeginCell().MustStoreUInt(0x70|(uint64(op.value.Int64())&0xF), 8)
	case bitsSz <= 8:
		return cell.BeginCell().MustStoreUInt(0x80, 8).MustStoreBigInt(op.value, 8)
	case bitsSz <= 16:
		return cell.BeginCell().MustStoreUInt(0x81, 8).MustStoreBigInt(op.value, 16)
	default:
		if bitsSz < 19 {
			bitsSz = 19
		}
		sz := uint64(bitsSz - 19) // 8*l = 256 - 19

		l := sz / 8
		if sz%8 != 0 {
			l += 1
		}

		x := 19 + l*8

		c := cell.BeginCell().
			MustStoreUInt(0x82, 8).
			MustStoreUInt(l, 5)

		if x > 257 {
			padBits := uint(x - 257)
			if op.value.Sign() < 0 {
				c.MustStoreUInt((uint64(1)<<padBits)-1, padBits)
			} else {
				c.MustStoreUInt(0, padBits)
			}
			x = 257
		}

		c.MustStoreBigInt(op.value, uint(x))

		return c
	}
}

func (op *OpPUSHINT) SerializeText() string {
	if op.value == nil {
		return "PUSHINT"
	}
	return fmt.Sprintf("%s PUSHINT", op.value.String())
}

func (op *OpPUSHINT) InstructionBits() int64 {
	if op.instructionBits != 0 {
		return op.instructionBits
	}
	if op.value == nil {
		return 8
	}

	bitsSz := signedBigIntBits(op.value)

	switch {
	case op.value.IsInt64() && op.value.Int64() >= -5 && op.value.Int64() <= 10:
		return 8
	case bitsSz <= 8:
		return 16
	case bitsSz <= 16:
		return 24
	default:
		return 13
	}
}

func (op *OpPUSHINT) Interpret(state *vm.State) error {
	if op.value.IsInt64() {
		val := op.value.Int64()
		if val >= -5 && val <= 10 {
			return state.Stack.PushSmallInt(val)
		}
	}
	return state.Stack.PushInt(op.value)
}
