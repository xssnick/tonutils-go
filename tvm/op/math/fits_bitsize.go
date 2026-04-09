package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return FITS(0) },
		func() vm.OP { return UFITS(0) },
		func() vm.OP { return FITSX() },
		func() vm.OP { return UFITSX() },
		func() vm.OP { return BITSIZE() },
		func() vm.OP { return UBITSIZE() },
		func() vm.OP { return QFITS(0) },
		func() vm.OP { return QUFITS(0) },
		func() vm.OP { return QFITSX() },
		func() vm.OP { return QUFITSX() },
		func() vm.OP { return QBITSIZE() },
		func() vm.OP { return QUBITSIZE() },
	)
}

func fitTinyOp(name string, prefix helpers.BitPrefix, bits uint8, unsigned, quiet bool) *helpers.AdvancedOP {
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

			width := int(bits) + 1
			if unsigned {
				if !unsignedFitsBits(x, width) {
					x = nil
				}
			} else if !signedFitsBits(x, width) {
				x = nil
			}
			return pushMaybeInt(state, x, quiet)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%s %d", name, int(bits)+1)
		},
		BitPrefix: helpers.BytesPrefix(prefix.Data...),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(bits), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			bits = uint8(v)
			return nil
		},
	}
}

func fitStackOp(name string, prefix helpers.BitPrefix, unsigned, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bits, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}

			width := int(bits.Int64())
			if unsigned {
				if !unsignedFitsBits(x, width) {
					x = nil
				}
			} else if !signedFitsBits(x, width) {
				x = nil
			}
			return pushMaybeInt(state, x, quiet)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func bitSizeOp(name string, prefix helpers.BitPrefix, signed, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			size := bitSizeInvalid
			if signed {
				size = signedBitSize(x)
			} else {
				size = unsignedBitSize(x)
			}
			if size == bitSizeInvalid {
				if quiet {
					return pushNaNOrOverflow(state, true)
				}
				return vmerr.Error(vmerr.CodeRangeCheck, "CHKSIZE for negative integer")
			}
			return pushSmallInt(state, int64(size))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func FITS(bits uint8) *helpers.AdvancedOP {
	return fitTinyOp("FITS", helpers.BytesPrefix(0xB4), bits, false, false)
}

func UFITS(bits uint8) *helpers.AdvancedOP {
	return fitTinyOp("UFITS", helpers.BytesPrefix(0xB5), bits, true, false)
}

func FITSX() *helpers.SimpleOP {
	return fitStackOp("FITSX", helpers.BytesPrefix(0xB6, 0x00), false, false)
}

func UFITSX() *helpers.SimpleOP {
	return fitStackOp("UFITSX", helpers.BytesPrefix(0xB6, 0x01), true, false)
}

func BITSIZE() *helpers.SimpleOP {
	return bitSizeOp("BITSIZE", helpers.BytesPrefix(0xB6, 0x02), true, false)
}

func UBITSIZE() *helpers.SimpleOP {
	return bitSizeOp("UBITSIZE", helpers.BytesPrefix(0xB6, 0x03), false, false)
}

func QFITS(bits uint8) *helpers.AdvancedOP {
	return fitTinyOp("QFITS", helpers.BytesPrefix(0xB7, 0xB4), bits, false, true)
}

func QUFITS(bits uint8) *helpers.AdvancedOP {
	return fitTinyOp("QUFITS", helpers.BytesPrefix(0xB7, 0xB5), bits, true, true)
}

func QFITSX() *helpers.SimpleOP {
	return fitStackOp("QFITSX", helpers.BytesPrefix(0xB7, 0xB6, 0x00), false, true)
}

func QUFITSX() *helpers.SimpleOP {
	return fitStackOp("QUFITSX", helpers.BytesPrefix(0xB7, 0xB6, 0x01), true, true)
}

func QBITSIZE() *helpers.SimpleOP {
	return bitSizeOp("QBITSIZE", helpers.BytesPrefix(0xB7, 0xB6, 0x02), true, true)
}

func QUBITSIZE() *helpers.SimpleOP {
	return bitSizeOp("QUBITSIZE", helpers.BytesPrefix(0xB7, 0xB6, 0x03), false, true)
}
