package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func registerInt8Variant(makeOP func(int8) *helpers.AdvancedOP, prefix uint64, prefixBits uint, value int8) {
	raw := uint8(value)
	vm.List = append(vm.List, func() vm.OP {
		return helpers.FullOpcodeVariant(makeOP(value), helpers.UIntPrefix(prefix<<8|uint64(raw), prefixBits+8))
	})
}
