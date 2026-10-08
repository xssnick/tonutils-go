package math

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, lessIntOp)
}

var lessIntOp = intCmpOp("LESSINT", 0xC1, intCmpMaskLess)

func LESSINT(value int8) vm.OP {
	return vm.Bind(lessIntOp, uint64(uint8(value)))
}
