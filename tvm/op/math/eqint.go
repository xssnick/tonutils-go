package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, eqIntOp)
}

var eqIntOp = intCmpOp("EQINT", 0xC0, intCmpMaskEqual)

func EQINT(value int8) vm.OP {
	return vm.Bind(eqIntOp, uint64(uint8(value)))
}
