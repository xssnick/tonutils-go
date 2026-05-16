package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestBinaryCompareOpsUseTVMStackOrder(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
		x    int64
		y    int64
		want bool
	}{
		{name: "LESS", op: LESS(), x: 1, y: 2, want: true},
		{name: "GREATER", op: GREATER(), x: 3, y: 2, want: true},
		{name: "LEQ", op: LEQ(), x: 2, y: 2, want: true},
		{name: "GEQ", op: GEQ(), x: 3, y: 2, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := vm.NewStack()
			if err := st.PushInt(big.NewInt(tt.x)); err != nil {
				t.Fatalf("failed to push x: %v", err)
			}
			if err := st.PushInt(big.NewInt(tt.y)); err != nil {
				t.Fatalf("failed to push y: %v", err)
			}

			if err := tt.op.Interpret(&vm.State{Stack: st}); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}

			got, err := st.PopBool()
			if err != nil {
				t.Fatalf("failed to pop result: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected result for %s: got=%t want=%t", tt.name, got, tt.want)
			}
		})
	}
}
