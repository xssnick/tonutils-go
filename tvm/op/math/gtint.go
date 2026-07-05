package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return GTINT(0) })
	// shared full-opcode variant for the common value
	vm.List = append(vm.List, func() vm.OP { return fixedIntCmpVariant(GTINT(4)) })
}

func GTINT(value int8) *OpIntCmp {
	return &OpIntCmp{
		name:   "GTINT",
		prefix: 0xC2,
		mask:   intCmpMaskGreater,
		value:  value,
	}
}
