package tvm

import (
	"math/big"
	"testing"
)

func TestTransactionCeilShiftRight(t *testing.T) {
	tests := []struct {
		name  string
		value int64
		bits  uint
		want  int64
	}{
		{name: "zero", value: 0, bits: 8, want: 0},
		{name: "no_shift", value: 7, bits: 0, want: 7},
		{name: "positive_exact", value: 256, bits: 8, want: 1},
		{name: "positive_rounded", value: 257, bits: 8, want: 2},
		{name: "negative_exact", value: -512, bits: 8, want: -2},
		{name: "negative_rounded", value: -513, bits: 8, want: -2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transactionCeilShiftRight(big.NewInt(tt.value), tt.bits)
			if got.Cmp(big.NewInt(tt.want)) != 0 {
				t.Fatalf("ceil shift = %s, want %d", got.String(), tt.want)
			}
		})
	}

	if got := transactionCeilShiftRight(nil, 8); got.Sign() != 0 {
		t.Fatalf("nil ceil shift = %s, want 0", got.String())
	}

	in := big.NewInt(257)
	got := transactionCeilShiftRight(in, 8)
	if got.Int64() != 2 {
		t.Fatalf("ceil shift = %s, want 2", got.String())
	}
	if in.Int64() != 257 {
		t.Fatalf("ceil shift mutated input to %s", in.String())
	}
}
