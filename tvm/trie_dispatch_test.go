package tvm

import (
	"testing"

	opstack "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMStepUsesBitTrieForNonByteAlignedOpcode(t *testing.T) {
	machine := NewTVM()

	state := &vm.State{
		CurrentCode: opstack.PUSHCTR(0).Serialize().EndCell().BeginParse(),
		Reg: vm.Register{
			C: [4]vm.Continuation{
				&vm.QuitContinuation{ExitCode: 7},
			},
		},
		Stack: vm.NewStack(),
		Gas:   vm.NewGas(),
	}

	if err := machine.step(state); err != nil {
		t.Fatal(err)
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatal(err)
	}

	quit, ok := cont.(*vm.QuitContinuation)
	if !ok {
		t.Fatalf("expected quit continuation, got %T", cont)
	}
	if quit.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", quit.ExitCode)
	}
}
