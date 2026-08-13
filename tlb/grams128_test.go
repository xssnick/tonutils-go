package tlb

import (
	"bytes"
	"math/big"
	"math/rand"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The fixed-width Grams path replaced a big.Int one on a consensus-critical
// hot path, so what these tests lock is not that it is correct in isolation but
// that it is indistinguishable from what it replaced — same bytes accepted,
// same bytes produced, same encodings rejected.

func gramsCases() []*big.Int {
	values := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(255),
		big.NewInt(256),
		big.NewInt(1_000_000_000),
		new(big.Int).SetUint64(1<<63 - 1),
		new(big.Int).SetUint64(1 << 63),
		new(big.Int).SetUint64(^uint64(0)),
	}
	// Every byte length the encoding allows, at both ends of its range.
	for ln := 1; ln <= 15; ln++ {
		low := new(big.Int).Lsh(big.NewInt(1), uint((ln-1)*8))
		high := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ln*8)), big.NewInt(1))
		values = append(values, low, high)
	}
	source := rand.New(rand.NewSource(20260809))
	for range 2000 {
		ln := source.Intn(15) + 1
		v := new(big.Int).Rand(source, new(big.Int).Lsh(big.NewInt(1), uint(ln*8)))
		values = append(values, v)
	}
	return values
}

func storeGramsBig(t *testing.T, v *big.Int) *cell.Slice {
	t.Helper()

	b := cell.BeginCell()
	if err := storeCanonicalGrams(b, v); err != nil {
		t.Fatalf("store %s: %v", v, err)
	}
	s, err := b.EndCell().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGramsU128RoundTripMatchesBigInt(t *testing.T) {
	for _, v := range gramsCases() {
		got, err := loadGramsU128(storeGramsBig(t, v))
		if err != nil {
			t.Fatalf("load %s: %v", v, err)
		}
		if want := new(big.Int).Or(new(big.Int).Lsh(new(big.Int).SetUint64(got.hi), 64),
			new(big.Int).SetUint64(got.lo)); want.Cmp(v) != 0 {
			t.Fatalf("loaded %s, want %s", want, v)
		}

		fixed := cell.BeginCell()
		if err = got.storeTo(fixed); err != nil {
			t.Fatalf("store back %s: %v", v, err)
		}
		reference := cell.BeginCell()
		if err = storeCanonicalGrams(reference, v); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixed.EndCell().ToBOC(), reference.EndCell().ToBOC()) {
			t.Fatalf("re-encoding %s does not match StoreBigVarUInt", v)
		}
	}
}

func TestGramsU128SumMatchesBigInt(t *testing.T) {
	source := rand.New(rand.NewSource(6519406))
	bound := new(big.Int).Lsh(big.NewInt(1), 119)
	for range 3000 {
		l := new(big.Int).Rand(source, bound)
		r := new(big.Int).Rand(source, bound)

		lv, err := loadGramsU128(storeGramsBig(t, l))
		if err != nil {
			t.Fatal(err)
		}
		rv, err := loadGramsU128(storeGramsBig(t, r))
		if err != nil {
			t.Fatal(err)
		}
		sum, err := lv.add(rv)
		if err != nil {
			t.Fatalf("add %s + %s: %v", l, r, err)
		}

		fixed := cell.BeginCell()
		if err = sum.storeTo(fixed); err != nil {
			t.Fatalf("store sum of %s and %s: %v", l, r, err)
		}
		reference := cell.BeginCell()
		if err = storeCanonicalGrams(reference, new(big.Int).Add(l, r)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixed.EndCell().ToBOC(), reference.EndCell().ToBOC()) {
			t.Fatalf("sum of %s and %s does not match the big.Int encoding", l, r)
		}
	}
}

// A leading zero byte is the one malformed shape the old path rejected by
// inspecting the raw bytes; the fixed-width path has no bytes to inspect and
// must reach the same verdict from the value alone.
func TestGramsU128RejectsLeadingZeroByte(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ln    uint64
		value uint64
		bits  uint
	}{
		{name: "one zero byte", ln: 1, value: 0, bits: 8},
		{name: "two bytes holding a one-byte value", ln: 2, value: 1, bits: 16},
		{name: "nine bytes holding an eight-byte value", ln: 9, value: 0, bits: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := cell.BeginCell()
			if err := b.StoreUInt(tc.ln, 4); err != nil {
				t.Fatal(err)
			}
			if tc.ln > 8 {
				if err := b.StoreUInt(tc.value, tc.bits); err != nil {
					t.Fatal(err)
				}
				if err := b.StoreUInt(1, 64); err != nil {
					t.Fatal(err)
				}
			} else if err := b.StoreUInt(tc.value, tc.bits); err != nil {
				t.Fatal(err)
			}
			s, err := b.EndCell().BeginParse()
			if err != nil {
				t.Fatal(err)
			}

			reference := *s
			if _, err = loadCanonicalGrams(&reference); err == nil {
				t.Fatal("the big.Int path accepted this encoding, so the test proves nothing")
			}
			if _, err = loadGramsU128(s); err == nil {
				t.Fatal("fixed-width path accepted a non-canonical encoding")
			}
		})
	}
}

func TestGramsU128RejectsOverflowingSum(t *testing.T) {
	max := gramsU128{hi: ^uint64(0), lo: ^uint64(0)}
	if _, err := max.add(gramsU128{lo: 1}); err == nil {
		t.Fatal("a sum past 128 bits must be reported, not wrapped")
	}
	// Sixteen bytes is one more than VarUInteger 16 admits, and the old path
	// refused it through StoreBigVarUInt's own length check.
	if err := max.storeTo(cell.BeginCell()); err == nil {
		t.Fatal("a value needing sixteen bytes must be refused")
	}
}
