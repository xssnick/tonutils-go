package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return PUSHPOW2(0) },
		func() vm.OP { return PUSHNAN() },
		func() vm.OP { return PUSHPOW2DEC(0) },
		func() vm.OP { return PUSHNEGPOW2(0) },
	)
}

func pushPowConst(name string, prefix byte, value uint8, fn func(int) *big.Int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("%s %d", name, int(value)+1)
		},
		BitPrefix:     helpers.BytesPrefix(prefix),
		FixedSizeBits: 8,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(value), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			value = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			return state.Stack.PushInt(fn(int(value) + 1))
		},
	}
}

func PUSHPOW2(value uint8) *helpers.AdvancedOP {
	return pushPowConst("PUSHPOW2", 0x83, value, func(x int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(x))
	})
}

func PUSHPOW2DEC(value uint8) *helpers.AdvancedOP {
	return pushPowConst("PUSHPOW2DEC", 0x84, value, func(x int) *big.Int {
		return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(x)), big.NewInt(1))
	})
}

func PUSHNEGPOW2(value uint8) *helpers.AdvancedOP {
	return pushPowConst("PUSHNEGPOW2", 0x85, value, func(x int) *big.Int {
		return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(x)))
	})
}

func PUSHNAN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.PushAny(vm.NaN{})
		},
		Name:      "PUSHNAN",
		BitPrefix: helpers.BytesPrefix(0x83, 0xFF),
	}
}
