package tvm

import (
	"math"
	"testing"
)

func TestTransactionExecutionLogicalTimeUint64Boundaries(t *testing.T) {
	tests := []struct {
		name             string
		prevLT           uint64
		configured       int64
		configuredUint64 uint64
		want             uint64
	}{
		{
			name:       "configured_max_int64",
			prevLT:     math.MaxUint64,
			configured: math.MaxInt64,
			want:       math.MaxInt64,
		},
		{
			name:             "full_width_configuration_overrides_legacy",
			prevLT:           1,
			configured:       -2,
			configuredUint64: uint64(math.MaxInt64) + 1,
			want:             uint64(math.MaxInt64) + 1,
		},
		{
			name:   "fallback_crosses_max_int64",
			prevLT: math.MaxInt64,
			want:   9_223_372_036_855_000_000,
		},
		{
			name:   "fallback_wraps_after_max_uint64",
			prevLT: math.MaxUint64,
			want:   448_384,
		},
		{
			name:       "negative_configuration_uses_wrapped_fallback",
			prevLT:     math.MaxUint64,
			configured: -1,
			want:       448_384,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transactionExecutionLogicalTime(tt.prevLT, tt.configured, tt.configuredUint64); got != tt.want {
				t.Fatalf("execution LT = %d, want %d", got, tt.want)
			}
		})
	}

	if got := transactionBlockLogicalTime(math.MaxUint64); got != 18_446_744_073_709_000_000 {
		t.Fatalf("block LT at MaxUint64 = %d", got)
	}
	fallback := transactionExecutionLogicalTime(math.MaxUint64, 0, 0)
	if got := transactionBlockLogicalTime(fallback); got != 0 {
		t.Fatalf("wrapped fallback block LT = %d, want 0", got)
	}
}

func TestBlockLogicalTimeUint64OverridePrecedence(t *testing.T) {
	legacy, err := emptyPreparedTestConfig().NewBlockContext(BlockOptions{
		BlockLT:  -7,
		RandSeed: make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.BlockLT() != -7 || legacy.BlockLTUint64() != 0 {
		t.Fatalf("legacy negative block LT = %d/%d, want -7/no override", legacy.BlockLT(), legacy.BlockLTUint64())
	}

	full := uint64(math.MaxInt64) + 1
	overridden, err := emptyPreparedTestConfig().NewBlockContext(BlockOptions{
		BlockLT:       -7,
		BlockLTUint64: full,
		RandSeed:      make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	if overridden.BlockLT() != -7 || overridden.BlockLTUint64() != full {
		t.Fatalf("overridden block LT = %d/%d, want legacy -7/full %d", overridden.BlockLT(), overridden.BlockLTUint64(), full)
	}
}

func TestTransactionValidateLogicalTimeRangeBoundaries(t *testing.T) {
	if err := transactionValidateLogicalTimeRange(math.MaxUint64-1, math.MaxUint64-1, math.MaxUint64); err != nil {
		t.Fatalf("last non-wrapping range was rejected: %v", err)
	}
	if err := transactionValidateLogicalTimeRange(math.MaxUint64, math.MaxUint64, 0); err == nil {
		t.Fatal("wrapped transaction end LT was accepted")
	}
	if err := transactionValidateLogicalTimeRange(2, 1, 3); err == nil {
		t.Fatal("start LT below account storage LT was accepted")
	}
}
