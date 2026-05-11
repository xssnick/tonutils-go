package tvm

import (
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	opcellslice "github.com/xssnick/tonutils-go/tvm/op/cellslice"
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

func TestTVMRegistersSDBEGINSConstPrefix(t *testing.T) {
	machine := NewTVM()
	code := opcellslice.SDBEGINSCONST(cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse(), false).Serialize().EndCell().BeginParse()

	if got := machine.matchOpcode(code); got == nil {
		t.Fatal("expected SDBEGINS const opcode to be registered in trie")
	}
}

func TestTVMMatchesWalletTraceSDBEGINSConstOpcode(t *testing.T) {
	machine := NewTVM()
	raw, err := hex.DecodeString("D72820761E436C20D749C008F2E09320D74AC002F2E09320D71D06C712C2005230B0F2D089D74CD7393001A4")
	if err != nil {
		t.Fatalf("decode trace opcode: %v", err)
	}
	code := cell.BeginCell().MustStoreSlice(raw, 352).EndCell().BeginParse()

	if got := machine.matchOpcode(code); got == nil {
		t.Fatal("expected wallet trace D728 opcode to be registered in trie")
	}
}
