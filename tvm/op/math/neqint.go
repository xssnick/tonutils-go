package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, neqIntOp)
}

var neqIntOp = intCmpOp("NEQINT", 0xC3, intCmpMaskLess|intCmpMaskGreater)

func NEQINT(value int8) vm.OP {
	return vm.Bind(neqIntOp, uint64(uint8(value)))
}
