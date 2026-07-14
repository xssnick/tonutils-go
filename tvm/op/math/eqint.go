package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EQINT(0) })
}

func EQINT(value int8) *OpIntCmp {
	return &OpIntCmp{
		name:   "EQINT",
		prefix: 0xC0,
		mask:   intCmpMaskEqual,
		value:  value,
	}
}
