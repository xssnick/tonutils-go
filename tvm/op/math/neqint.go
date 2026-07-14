package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return NEQINT(0) })
}

func NEQINT(value int8) *OpIntCmp {
	return &OpIntCmp{
		name:   "NEQINT",
		prefix: 0xC3,
		mask:   intCmpMaskLess | intCmpMaskGreater,
		value:  value,
	}
}
