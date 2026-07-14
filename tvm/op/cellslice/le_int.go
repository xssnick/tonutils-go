package cellslice

import (
	"encoding/binary"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

var cellsliceBigIntOne = big.NewInt(1)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return LDILE4() },
		func() vm.OP { return STILE4() },
	)
}

func loadLEModeName(mode uint64) string {
	name := "LDI"
	if mode&1 != 0 {
		name = "LDU"
	}
	name += "LE4"
	if mode&2 != 0 {
		name = name[:len(name)-1] + "8"
	}
	if mode&4 != 0 {
		name = "P" + name
	}
	if mode&8 != 0 {
		name += "Q"
	}
	return name
}

func storeLEModeName(mode uint64) string {
	name := "STI"
	if mode&1 != 0 {
		name = "STU"
	}
	name += "LE4"
	if mode&2 != 0 {
		name = name[:len(name)-1] + "8"
	}
	return name
}

func decodeLEInt(data []byte, unsigned bool) *big.Int {
	if unsigned {
		if len(data) == 4 {
			return new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(data)))
		}
		return new(big.Int).SetUint64(binary.LittleEndian.Uint64(data))
	}
	if len(data) == 4 {
		return big.NewInt(int64(int32(binary.LittleEndian.Uint32(data))))
	}
	return big.NewInt(int64(binary.LittleEndian.Uint64(data)))
}

func fitsUnsignedBits(x *big.Int, bits uint) bool {
	return x.Sign() >= 0 && x.BitLen() <= int(bits)
}

func fitsSignedBits(x *big.Int, bits uint) bool {
	if bits == 0 {
		return x.Sign() == 0
	}
	if x.Sign() >= 0 {
		return x.BitLen() <= int(bits-1)
	}

	absMinusOne := new(big.Int).Neg(x)
	absMinusOne.Sub(absMinusOne, cellsliceBigIntOne)
	return absMinusOne.BitLen() <= int(bits-1)
}

func encodeLEInt(x *big.Int, bytes int, unsigned bool) ([]byte, error) {
	out := make([]byte, bytes)
	if !writeLEInt(out, x, unsigned) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return out, nil
}

func writeLEInt(out []byte, x *big.Int, unsigned bool) bool {
	if unsigned {
		if x.Sign() < 0 {
			return false
		}

		switch len(out) {
		case 4:
			if x.BitLen() > 32 {
				return false
			}
			binary.LittleEndian.PutUint32(out, uint32(x.Uint64()))
		case 8:
			if x.BitLen() > 64 {
				return false
			}
			binary.LittleEndian.PutUint64(out, x.Uint64())
		default:
			return false
		}
		return true
	}

	if !x.IsInt64() {
		return false
	}
	value := x.Int64()
	switch len(out) {
	case 4:
		if int64(int32(value)) != value {
			return false
		}
		binary.LittleEndian.PutUint32(out, uint32(int32(value)))
	case 8:
		binary.LittleEndian.PutUint64(out, uint64(value))
	default:
		return false
	}
	return true
}

func loadLEIntOp(mode uint64) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string { return loadLEModeName(mode) },
		BitPrefix:      helpers.UIntPrefix(0xD75, 12),
		FixedSizeBits:  4,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(mode, 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			mode = v
			return nil
		},
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			bytesLen := 4
			if mode&2 != 0 {
				bytesLen = 8
			}
			preload := mode&4 != 0
			quiet := mode&8 != 0

			var data []byte
			if preload {
				data, err = cs.PreloadSlice(uint(bytesLen * 8))
			} else {
				data, err = cs.LoadSlice(uint(bytesLen * 8))
			}
			if err != nil {
				if !quiet {
					return vmerr.Error(vmerr.CodeCellUnderflow)
				}
				if !preload {
					if pushErr := state.Stack.PushOwnedSlice(cs); pushErr != nil {
						return pushErr
					}
				}
				return state.Stack.PushBool(false)
			}

			if err = state.Stack.PushInt(decodeLEInt(data, mode&1 != 0)); err != nil {
				return err
			}
			if !preload {
				if err = state.Stack.PushOwnedSlice(cs); err != nil {
					return err
				}
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
	}
}

func storeLEIntOp(mode uint64) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string { return storeLEModeName(mode) },
		BitPrefix:      helpers.UIntPrefix(0xCF28>>2, 14),
		FixedSizeBits:  2,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(mode, 2)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(2)
			if err != nil {
				return err
			}
			mode = v
			return nil
		},
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			bytesLen := 4
			if mode&2 != 0 {
				bytesLen = 8
			}
			if !builder.CanExtendBy(uint(bytesLen*8), 0) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if x == nil {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			var data [8]byte
			encoded := data[:bytesLen]
			if !writeLEInt(encoded, x, mode&1 != 0) {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			if err = builder.StoreSlice(encoded, uint(bytesLen*8)); err != nil {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return state.Stack.PushOwnedBuilder(builder)
		},
	}
}

func LDILE4() *helpers.AdvancedOP   { return loadLEIntOp(0) }
func LDULE4() *helpers.AdvancedOP   { return loadLEIntOp(1) }
func LDILE8() *helpers.AdvancedOP   { return loadLEIntOp(2) }
func LDULE8() *helpers.AdvancedOP   { return loadLEIntOp(3) }
func PLDILE4() *helpers.AdvancedOP  { return loadLEIntOp(4) }
func PLDULE4() *helpers.AdvancedOP  { return loadLEIntOp(5) }
func PLDILE8() *helpers.AdvancedOP  { return loadLEIntOp(6) }
func PLDULE8() *helpers.AdvancedOP  { return loadLEIntOp(7) }
func LDILE4Q() *helpers.AdvancedOP  { return loadLEIntOp(8) }
func LDULE4Q() *helpers.AdvancedOP  { return loadLEIntOp(9) }
func LDILE8Q() *helpers.AdvancedOP  { return loadLEIntOp(10) }
func LDULE8Q() *helpers.AdvancedOP  { return loadLEIntOp(11) }
func PLDILE4Q() *helpers.AdvancedOP { return loadLEIntOp(12) }
func PLDULE4Q() *helpers.AdvancedOP { return loadLEIntOp(13) }
func PLDILE8Q() *helpers.AdvancedOP { return loadLEIntOp(14) }
func PLDULE8Q() *helpers.AdvancedOP { return loadLEIntOp(15) }

func STILE4() *helpers.AdvancedOP { return storeLEIntOp(0) }
func STULE4() *helpers.AdvancedOP { return storeLEIntOp(1) }
func STILE8() *helpers.AdvancedOP { return storeLEIntOp(2) }
func STULE8() *helpers.AdvancedOP { return storeLEIntOp(3) }
