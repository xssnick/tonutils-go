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
	vm.List = append(vm.List,
		func() vm.OP { return storeIntVarExtOp(0) },
		func() vm.OP { return storeIntFixedExtOp(0, 1) },
	)
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

	limit := new(big.Int).Lsh(big.NewInt(1), bits-1)
	min := new(big.Int).Neg(limit)
	return x.Cmp(min) >= 0
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
		if err := state.Stack.PushBuilder(builder); err != nil {
			return err
		}
		if err := pushStoreIntValue(state, x); err != nil {
			return err
		}
	} else {
		if err := pushStoreIntValue(state, x); err != nil {
			return err
		}
		if err := state.Stack.PushBuilder(builder); err != nil {
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

	if err = state.Stack.PushBuilder(builder); err != nil {
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

func storeIntVarExtOp(mode uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		BitPrefix:     helpers.UIntPrefix(0xCF00>>3, 13),
		FixedSizeBits: 3,
		NameSerializer: func() string {
			return storeIntExtName(mode, nil, true)
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(mode), 3)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(3)
			if err != nil {
				return err
			}
			mode = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			maxBits := int64(256)
			if mode&1 == 0 {
				maxBits = 257
			}
			bits, err := state.Stack.PopIntRange(0, maxBits)
			if err != nil {
				return err
			}
			return storeIntExtCommon(state, uint(bits.Uint64()), mode)
		},
	}
}

func storeIntFixedExtOp(mode uint8, bits uint) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		BitPrefix:     helpers.UIntPrefix(0xCF08>>3, 13),
		FixedSizeBits: 11,
		NameSerializer: func() string {
			return storeIntExtName(mode, &bits, false)
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(mode), 3).
				MustStoreUInt(uint64(bits-1), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(11)
			if err != nil {
				return err
			}
			mode = uint8((v >> 8) & 0x7)
			bits = uint(v&0xff) + 1
			return nil
		},
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			return storeIntExtCommon(state, bits, mode)
		},
	}
}
