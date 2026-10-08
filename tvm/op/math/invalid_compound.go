package math

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		newInvalidCompoundFamily("DIV/MOD<invalid>", helpers.UIntPrefix(0xA90, 12), 4, 0x3),
		newInvalidCompoundFamily("SHR/MOD<invalid>", helpers.UIntPrefix(0xA92, 12), 4, 0x7),
		newInvalidCompoundFamily("SHR#/MOD<invalid>", helpers.UIntPrefix(0xA93, 12), 12, 0x300),
		newInvalidCompoundFamily("MULDIV/MOD<invalid>", helpers.UIntPrefix(0xA98, 12), 4, 0x7),
		newInvalidCompoundFamily("MULSHR/MOD<invalid>", helpers.UIntPrefix(0xA9A, 12), 4, 0x7),
		newInvalidCompoundFamily("MULSHR#/MOD<invalid>", helpers.UIntPrefix(0xA9B, 12), 12, 0x300),
		newInvalidCompoundFamily("SHLDIV/MOD<invalid>", helpers.UIntPrefix(0xA9C, 12), 4, 0x7),
		newInvalidCompoundFamily("SHLDIV#/MOD<invalid>", helpers.UIntPrefix(0xA9D, 12), 12, 0x300),
	)
}

// newInvalidCompoundFamily covers the whole suffix range of a compound family
// that the reference VM never assigned. The suffix is consumed so the truncated
// instruction is charged for correctly, but it is discarded: re-encoding always
// writes the family's canonical suffix rather than whatever was decoded.
func newInvalidCompoundFamily(name string, prefix helpers.BitPrefix, suffixBits uint, suffix uint64) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(prefix),
		ArgBits:  suffixBits,
		Action: func(state *vm.State, args uint64) error {
			return vmerr.Error(vmerr.CodeInvalidOpcode)
		},
		Serializer: func(args uint64) *cell.Builder {
			return cell.BeginCell().
				MustStoreSlice(prefix.Data, prefix.Bits).
				MustStoreUInt(suffix, suffixBits)
		},
		Name: func(args uint64) string {
			return name
		},
	})
}

func invalidCompoundFamily(name string, prefix helpers.BitPrefix, suffixBits uint, suffix uint64) vm.OP {
	return vm.Bind(newInvalidCompoundFamily(name, prefix, suffixBits, suffix), suffix)
}
