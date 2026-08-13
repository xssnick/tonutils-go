package math

import "testing"

func TestVariableMulShiftModSerializeTextParity(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   interface{ SerializeText() string }
	}{
		{name: "MULMODPOW2", op: MULMODPOW2_VAR()},
		{name: "MULMODPOW2R", op: MULMODPOW2R_VAR()},
		{name: "MULMODPOW2C", op: MULMODPOW2C_VAR()},
		{name: "MULRSHIFTMOD", op: MULRSHIFTMOD_VAR()},
		{name: "MULRSHIFTRMOD", op: MULRSHIFTRMOD_VAR()},
		{name: "MULRSHIFTCMOD", op: MULRSHIFTCMOD_VAR()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.op.SerializeText(); got != tt.name {
				t.Fatalf("SerializeText() = %q, want %q", got, tt.name)
			}
		})
	}
}
