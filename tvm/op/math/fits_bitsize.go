package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return FITSX() },
		func() vm.OP { return UFITSX() },
		func() vm.OP { return BITSIZE() },
		func() vm.OP { return UBITSIZE() },
		func() vm.OP { return QFITSX() },
		func() vm.OP { return QUFITSX() },
		func() vm.OP { return QBITSIZE() },
		func() vm.OP { return QUBITSIZE() },
	)
	vm.ArgList = append(vm.ArgList, fitsOp, ufitsOp, qfitsOp, qufitsOp)
}

// fitTinyOp builds a FITS-style opcode: the encoded byte holds width-1, so the
// checked width is 1..256.
func fitTinyOp(name string, prefix helpers.BitPrefix, unsigned, quiet bool) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(prefix),
		ArgBits:  8,
		Action: func(state *vm.State, args uint64) error {
			x, err := popIntOperandRead(state, quiet)
			if err != nil {
				return err
			}
			if x == nil {
				return pushNaNOrOverflow(state, quiet)
			}

			width := bytePlusOneValue(args)
			if unsigned {
				if !unsignedFitsBits(x, width) {
					x = nil
				}
			} else if !signedFitsBits(x, width) {
				x = nil
			}
			return pushMaybeInt(state, x, quiet)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("%s %d", name, bytePlusOneValue(args))
		},
	})
}

func fitStackOp(name string, prefix helpers.BitPrefix, unsigned, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			bits, err := popIntRange(state, 0, 1023)
			if err != nil {
				return err
			}
			x, err := popIntOperandRead(state, quiet)
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
			x, err := popIntOperandRead(state, quiet)
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

var (
	fitsOp   = fitTinyOp("FITS", helpers.BytesPrefix(0xB4), false, false)
	ufitsOp  = fitTinyOp("UFITS", helpers.BytesPrefix(0xB5), true, false)
	qfitsOp  = fitTinyOp("QFITS", helpers.BytesPrefix(0xB7, 0xB4), false, true)
	qufitsOp = fitTinyOp("QUFITS", helpers.BytesPrefix(0xB7, 0xB5), true, true)
)

func FITS(bits uint8) vm.OP {
	return vm.Bind(fitsOp, uint64(bits))
}

func UFITS(bits uint8) vm.OP {
	return vm.Bind(ufitsOp, uint64(bits))
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

func QFITS(bits uint8) vm.OP {
	return vm.Bind(qfitsOp, uint64(bits))
}

func QUFITS(bits uint8) vm.OP {
	return vm.Bind(qufitsOp, uint64(bits))
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
