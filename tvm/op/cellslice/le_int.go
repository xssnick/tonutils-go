package cellslice

import (
	"encoding/binary"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

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
	max := new(big.Int).Lsh(big.NewInt(1), bits-1)
	min := new(big.Int).Neg(new(big.Int).Set(max))
	max.Sub(max, big.NewInt(1))
	return x.Cmp(min) >= 0 && x.Cmp(max) <= 0
}

func encodeLEInt(x *big.Int, bytes int, unsigned bool) ([]byte, error) {
	out := make([]byte, bytes)
	bits := uint(bytes * 8)
	if unsigned {
		if !fitsUnsignedBits(x, bits) {
			return nil, vmerr.Error(vmerr.CodeRangeCheck)
		}
		if bytes == 4 {
			binary.LittleEndian.PutUint32(out, uint32(x.Uint64()))
		} else {
			binary.LittleEndian.PutUint64(out, x.Uint64())
		}
		return out, nil
	}
	if !fitsSignedBits(x, bits) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	if bytes == 4 {
		binary.LittleEndian.PutUint32(out, uint32(int32(x.Int64())))
	} else {
		binary.LittleEndian.PutUint64(out, uint64(x.Int64()))
	}
	return out, nil
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
					if pushErr := state.Stack.PushSlice(cs); pushErr != nil {
						return pushErr
					}
				}
				return state.Stack.PushBool(false)
			}

			if err = state.Stack.PushInt(decodeLEInt(data, mode&1 != 0)); err != nil {
				return err
			}
			if !preload {
				if err = state.Stack.PushSlice(cs); err != nil {
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
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
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
			data, err := encodeLEInt(x, bytesLen, mode&1 != 0)
			if err != nil {
				return err
			}
			if err = builder.StoreSlice(data, uint(bytesLen*8)); err != nil {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return state.Stack.PushBuilder(builder)
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
