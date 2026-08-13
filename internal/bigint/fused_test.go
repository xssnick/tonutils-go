package bigint

import (
	"math/big"
	"testing"
)

func TestFusedValuesMatchPlainBigInt(t *testing.T) {
	cases := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(-1),
		big.NewInt(1 << 62),
		new(big.Int).SetUint64(^uint64(0)),
		new(big.Int).Lsh(big.NewInt(1), 127),
		new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 200)),
	}
	for _, want := range cases {
		if got := Set(want); got.Cmp(want) != 0 {
			t.Fatalf("Set(%s) = %s", want, got)
		}
	}
	if got := FromInt64(-42); got.Int64() != -42 {
		t.Fatalf("FromInt64 = %s", got)
	}
	if got := FromUint64(^uint64(0)); got.Uint64() != ^uint64(0) {
		t.Fatalf("FromUint64 = %s", got)
	}
	if got := New(); got.Sign() != 0 {
		t.Fatalf("New = %s", got)
	}
}

// A value that outgrows the inline array must keep working; it just stops
// being inline.
func TestFusedGrowsBeyondInlineStorage(t *testing.T) {
	v := FromUint64(1)
	want := big.NewInt(1)
	for i := 0; i < 40; i++ {
		v.Mul(v, big.NewInt(1<<40))
		want.Mul(want, big.NewInt(1<<40))
		if v.Cmp(want) != 0 {
			t.Fatalf("after %d multiplications: %s != %s", i, v, want)
		}
	}
}

// Two fused values must not share digits.
func TestFusedValuesAreIndependent(t *testing.T) {
	a := FromInt64(7)
	b := Set(a)
	b.Add(b, big.NewInt(1))
	if a.Int64() != 7 {
		t.Fatalf("copy aliased the source: %s", a)
	}
}

func TestFusedSetCostsOneAllocation(t *testing.T) {
	src := new(big.Int).SetUint64(1 << 40)
	var sink *big.Int
	fused := testing.AllocsPerRun(200, func() { sink = Set(src) })
	plain := testing.AllocsPerRun(200, func() { sink = new(big.Int).Set(src) })
	_ = sink
	if fused >= plain {
		t.Fatalf("fused Set allocated %.0f objects, plain allocated %.0f — fusion bought nothing", fused, plain)
	}
	if fused != 1 {
		t.Fatalf("fused Set = %.0f allocations, want 1", fused)
	}
}

func TestFusedFromUint64CostsOneAllocation(t *testing.T) {
	var sink *big.Int
	fused := testing.AllocsPerRun(200, func() { sink = FromUint64(1 << 40) })
	plain := testing.AllocsPerRun(200, func() { sink = new(big.Int).SetUint64(1 << 40) })
	_ = sink
	if fused >= plain {
		t.Fatalf("fused FromUint64 allocated %.0f objects, plain allocated %.0f", fused, plain)
	}
}
