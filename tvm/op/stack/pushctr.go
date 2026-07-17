package stack

import execop "github.com/xssnick/tonutils-go/tvm/op/exec"

type OpPUSHCTR = execop.OpPUSHCTR

// Deprecated: use exec.PUSHCTR. Both constructors return the canonical opcode
// implementation registered for ED40..ED45 and ED47.
func PUSHCTR(ctrIndex uint8) *OpPUSHCTR {
	return execop.PUSHCTR(int(ctrIndex))
}
