package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

type OpPUSHINT struct {
	value *big.Int
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHINT(nil) })
}

func PUSHINT(value *big.Int) *OpPUSHINT {
	return &OpPUSHINT{
		value: value,
	}
}

func (op *OpPUSHINT) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		cell.BeginCell().MustStoreUInt(0x7, 4).ToSlice(),
		cell.BeginCell().MustStoreUInt(0x80, 8).EndCell().BeginParse(),
		cell.BeginCell().MustStoreUInt(0x81, 8).EndCell().BeginParse(),
		cell.BeginCell().MustStoreUInt(0x82, 8).EndCell().BeginParse(),
	}
}

func (op *OpPUSHINT) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	switch prefix {
	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f:
		op.value = big.NewInt(int64(prefix - 0x70))
		return nil
	case 0x80:
		val, err := code.LoadBigInt(8)
		if err != nil {
			return err
		}
		op.value = val
		return nil
	case 0x81:
		val, err := code.LoadBigInt(16)
		if err != nil {
			return err
		}
		op.value = val
		return nil
	case 0x82:
		szBytes, err := code.LoadUInt(5)
		if err != nil {
			return err
		}

		sz := szBytes*8 + 19

		if sz > 257 {
			_, err = code.LoadUInt(uint(sz - 257)) // kill round bits
			if err != nil {
				return err
			}
			sz = 257
		}

		val, err := code.LoadBigInt(uint(sz))
		if err != nil {
			return err
		}

		op.value = val
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPUSHINT) Serialize() *cell.Builder {
	bitsSz := op.value.BitLen() + 1 // 1 bit for sign

	switch {
	case op.value.BitLen() < 4 && op.value.Sign() >= 0:
		return cell.BeginCell().MustStoreUInt(0x70|op.value.Uint64(), 8)
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

		if x > 256 {
			c.MustStoreUInt(0, uint(x-256))
			x = 256
		}

		c.MustStoreBigInt(op.value, uint(x))

		return c
	}
}

func (op *OpPUSHINT) SerializeText() string {
	return fmt.Sprintf("%s PUSHINT", op.value.String())
}

func (op *OpPUSHINT) Interpret(state *vm.State) error {
	return state.Stack.PushInt(new(big.Int).Set(op.value))
}
