package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, storeIntVarExtSharedOp, storeIntFixedExtSharedOp)
}

func signedStoreFits(x *big.Int, bits uint) bool {
	if x == nil {
		return false
	}
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

func unsignedStoreFits(x *big.Int, bits uint) bool {
	if x == nil || x.Sign() < 0 {
		return false
	}
	if bits == 0 {
		return x.Sign() == 0
	}
	return x.BitLen() <= int(bits)
}

func pushStoreIntValue(state *vm.State, x *big.Int) error {
	if x == nil {
		return state.Stack.PushAny(vm.NaN{})
	}
	return state.Stack.PushIntQuiet(x)
}

func storeIntQuietFail(state *vm.State, status int64, builder *cell.Builder, x *big.Int, reverse bool) error {
	if reverse {
		if err := state.Stack.PushOwnedBuilder(builder); err != nil {
			return err
		}
		if err := pushStoreIntValue(state, x); err != nil {
			return err
		}
	} else {
		if err := pushStoreIntValue(state, x); err != nil {
			return err
		}
		if err := state.Stack.PushOwnedBuilder(builder); err != nil {
			return err
		}
	}
	return pushBuilderInt(state, status)
}

func storeIntExtCommon(state *vm.State, bits uint, mode uint8) error {
	signed := mode&1 == 0
	reverse := mode&2 != 0
	quiet := mode&4 != 0

	var builder *cell.Builder
	var x *big.Int
	var err error
	if reverse {
		x, err = state.Stack.PopInt()
		if err != nil {
			return err
		}
		builder, err = state.Stack.PopBuilder()
		if err != nil {
			return err
		}
	} else {
		builder, err = state.Stack.PopBuilder()
		if err != nil {
			return err
		}
		x, err = state.Stack.PopInt()
		if err != nil {
			return err
		}
	}

	if !builder.CanExtendBy(bits, 0) {
		if quiet {
			return storeIntQuietFail(state, -1, builder, x, reverse)
		}
		return vmerr.Error(vmerr.CodeCellOverflow)
	}

	fits := unsignedStoreFits(x, bits)
	if signed {
		fits = signedStoreFits(x, bits)
	}
	if !fits {
		if quiet {
			return storeIntQuietFail(state, 1, builder, x, reverse)
		}
		return vmerr.Error(vmerr.CodeRangeCheck)
	}

	if signed {
		err = builder.StoreBigInt(x, bits)
	} else {
		err = builder.StoreBigUInt(x, bits)
	}
	if err != nil {
		if quiet {
			return storeIntQuietFail(state, 1, builder, x, reverse)
		}
		return vmerr.Error(vmerr.CodeRangeCheck, err.Error())
	}

	if err = state.Stack.PushOwnedBuilder(builder); err != nil {
		return err
	}
	if quiet {
		return pushBuilderInt(state, 0)
	}
	return nil
}

func storeIntExtName(mode uint8, bits *uint, variable bool) string {
	name := "ST"
	if mode&1 == 0 {
		name += "I"
	} else {
		name += "U"
	}
	if variable {
		name += "X"
	}
	if mode&2 != 0 {
		name += "R"
	}
	if mode&4 != 0 {
		name += "Q"
	}
	if !variable {
		name += fmt.Sprintf(" %d", *bits)
	}
	return name
}

var storeIntVarExtSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xCF00>>3, 13)),
	ArgBits:  3,
	Name: func(args uint64) string {
		return storeIntExtName(uint8(args), nil, true)
	},
	Action: func(state *vm.State, args uint64) error {
		if state.Stack.Len() < 3 {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}
		mode := uint8(args)
		maxBits := int64(256)
		if mode&1 == 0 {
			maxBits = 257
		}
		bits, err := state.Stack.PopIntRangeInt64(0, maxBits)
		if err != nil {
			return err
		}
		return storeIntExtCommon(state, uint(bits), mode)
	},
})

// The 11-bit operand is the mode in bits 8..10 and the width minus one in the
// low byte, exactly as the instruction encodes it.
var storeIntFixedExtSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xCF08>>3, 13)),
	ArgBits:  11,
	Name: func(args uint64) string {
		bits := uint(args&0xff) + 1
		return storeIntExtName(uint8((args>>8)&0x7), &bits, false)
	},
	Action: func(state *vm.State, args uint64) error {
		if state.Stack.Len() < 2 {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}
		return storeIntExtCommon(state, uint(args&0xff)+1, uint8((args>>8)&0x7))
	},
})

func storeIntVarExtOp(mode uint8) vm.OP {
	return vm.Bind(storeIntVarExtSharedOp, uint64(mode))
}

func storeIntFixedExtOp(mode uint8, bits uint) vm.OP {
	return vm.Bind(storeIntFixedExtSharedOp, uint64(mode)<<8|uint64(bits-1))
}
