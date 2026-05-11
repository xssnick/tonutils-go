package exec

import "testing"

func TestExecAdvancedOpsInstructionBits(t *testing.T) {
	tests := []struct {
		name string
		got  int64
		want int64
	}{
		{name: "JMPXARGS", got: JMPXARGS(3).InstructionBits(), want: 16},
		{name: "THROWANY", got: newThrowAny().InstructionBits(), want: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("instruction bits mismatch: got %d want %d", tt.got, tt.want)
			}
		})
	}
}
