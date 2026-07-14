package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestDICTIGETJMPZShortStackDoesNotConsumeValue(t *testing.T) {
	state := newTestState()
	if err := state.Stack.PushInt(big.NewInt(1024)); err != nil {
		t.Fatalf("push value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1024)); err != nil {
		t.Fatalf("push value: %v", err)
	}

	assertVMErrCode(t, DICTIGETJMPZ().Interpret(state), vmerr.CodeStackUnderflow)
	if state.Stack.Len() != 2 {
		t.Fatalf("expected short stack to remain unchanged, stack len=%d", state.Stack.Len())
	}
}
