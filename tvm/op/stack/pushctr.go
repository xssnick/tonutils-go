package stack

import (
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// Deprecated: use exec.PUSHCTR. Both constructors return the canonical opcode
// implementation registered for ED40..ED45 and ED47.
func PUSHCTR(ctrIndex uint8) vm.OP {
	return execop.PUSHCTR(int(ctrIndex))
}
