package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, gtIntOp)
}

var gtIntOp = intCmpOp("GTINT", 0xC2, intCmpMaskGreater)

func GTINT(value int8) vm.OP {
	return vm.Bind(gtIntOp, uint64(uint8(value)))
}
