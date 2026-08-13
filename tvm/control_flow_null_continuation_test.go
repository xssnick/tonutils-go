package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestHandleStepErrorDoesNotRouteThroughTypedNilC2(t *testing.T) {
	var c2 *vm.QuitContinuation
	state := &vm.State{
		CurrentCode: cell.BeginCell().ToSlice(),
		Reg:         vm.Register{C: [4]vm.Continuation{nil, nil, c2}},
		Stack:       vm.NewStack(),
	}
	original := vmerr.Error(vmerr.CodeTypeCheck)

	retry, err := new(TVM).handleStepError(state, original)
	if retry {
		t.Fatal("typed-nil c2 unexpectedly requested exception retry")
	}
	if got, ok := vmerr.AsVMError(err); !ok || got.Code != vmerr.CodeTypeCheck {
		t.Fatalf("error = %v, want original type-check", err)
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("stack depth = %d, want no exception-frame mutation", state.Stack.Len())
	}
}
