package math

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return invalidCompoundFamily("DIV/MOD<invalid>", helpers.UIntPrefix(0xA90, 12), 4, 0x3) },
		func() vm.OP { return invalidCompoundFamily("SHR/MOD<invalid>", helpers.UIntPrefix(0xA92, 12), 4, 0x7) },
		func() vm.OP {
			return invalidCompoundFamily("SHR#/MOD<invalid>", helpers.UIntPrefix(0xA93, 12), 12, 0x300)
		},
		func() vm.OP {
			return invalidCompoundFamily("MULDIV/MOD<invalid>", helpers.UIntPrefix(0xA98, 12), 4, 0x7)
		},
		func() vm.OP {
			return invalidCompoundFamily("MULSHR/MOD<invalid>", helpers.UIntPrefix(0xA9A, 12), 4, 0x7)
		},
		func() vm.OP {
			return invalidCompoundFamily("MULSHR#/MOD<invalid>", helpers.UIntPrefix(0xA9B, 12), 12, 0x300)
		},
		func() vm.OP {
			return invalidCompoundFamily("SHLDIV/MOD<invalid>", helpers.UIntPrefix(0xA9C, 12), 4, 0x7)
		},
		func() vm.OP {
			return invalidCompoundFamily("SHLDIV#/MOD<invalid>", helpers.UIntPrefix(0xA9D, 12), 12, 0x300)
		},
	)
}

func invalidCompoundFamily(name string, prefix helpers.BitPrefix, suffixBits uint, suffix uint64) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: int64(suffixBits),
		Action: func(state *vm.State) error {
			return vmerr.Error(vmerr.CodeInvalidOpcode)
		},
		NameSerializer: func() string {
			return name
		},
		BitPrefix: prefix,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(suffix, suffixBits)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			_, err := code.LoadUInt(suffixBits)
			return err
		},
	}
}
