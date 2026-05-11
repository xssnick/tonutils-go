package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExcQuitContinuationReturnsOriginalCode(t *testing.T) {
	state := &State{Stack: NewStack()}
	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push exception code: %v", err)
	}

	_, err := (&ExcQuitContinuation{}).Jump(state)
	if err == nil {
		t.Fatal("expected exception exit")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected VMError, got %T", err)
	}
	var handled HandledException
	if !errors.As(err, &handled) {
		t.Fatalf("expected handled exception wrapper, got %T", err)
	}
	if vmErr.Code != 55 {
		t.Fatalf("expected exit code 55, got %d", vmErr.Code)
	}
}
