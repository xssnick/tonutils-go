package exec

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

// instructionBits reads the charged length off an opcode that prices itself.
func instructionBits(t *testing.T, op vm.OP) int64 {
	t.Helper()

	priced, ok := op.(vm.GasPricedOp)
	if !ok {
		t.Fatalf("opcode %T does not price its instruction", op)
	}
	return priced.InstructionBits()
}

func TestExecAdvancedOpsInstructionBits(t *testing.T) {
	tests := []struct {
		name string
		got  int64
		want int64
	}{
		{name: "JMPXARGS", got: instructionBits(t, JMPXARGS(3)), want: 16},
		{name: "THROWANY", got: instructionBits(t, vm.Bind(throwAnyOp, 0)), want: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("instruction bits mismatch: got %d want %d", tt.got, tt.want)
			}
		})
	}
}
