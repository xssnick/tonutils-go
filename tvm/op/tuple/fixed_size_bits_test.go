package tuple

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTupleAdvancedOpsInstructionBits(t *testing.T) {
	tests := []struct {
		name string
		op   vm.GasPricedOp
		bits int64
	}{
		{name: "TUPLE", op: TUPLE(2), bits: 16},
		{name: "INDEX", op: INDEX(1), bits: 16},
		{name: "INDEXQ", op: INDEXQ(1), bits: 16},
		{name: "SETINDEX", op: SETINDEX(1), bits: 16},
		{name: "SETINDEXQ", op: SETINDEXQ(1), bits: 16},
		{name: "EXPLODE", op: EXPLODE(2), bits: 16},
		{name: "UNTUPLE", op: UNTUPLE(2), bits: 16},
		{name: "UNPACKFIRST", op: UNPACKFIRST(2), bits: 16},
		{name: "INDEX2", op: INDEX2(1, 2), bits: 16},
		{name: "INDEX3", op: INDEX3(0, 1, 2), bits: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.op.InstructionBits(); got != tt.bits {
				t.Fatalf("instruction bits mismatch: got %d want %d", got, tt.bits)
			}
		})
	}
}
