package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LESSINT(0) })
	// shared full-opcode variant for the common value
	vm.List = append(vm.List, func() vm.OP { return fixedIntCmpVariant(LESSINT(5)) })
}

func LESSINT(value int8) *OpIntCmp {
	return &OpIntCmp{
		name:   "LESSINT",
		prefix: 0xC1,
		mask:   intCmpMaskLess,
		value:  value,
	}
}
