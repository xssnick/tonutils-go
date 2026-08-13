package cell

import (
	"math/big"
	"testing"
)

func TestFixedDictNodeCanonicalLabelDetection(t *testing.T) {
	tests := []struct {
		name      string
		label     *Builder
		maxLen    uint
		canonical bool
	}{
		{name: "short-zero", label: rawShortDictLabel(0, 0), maxLen: 8, canonical: true},
		{name: "long-zero", label: rawLongDictLabel(0, 0, 8), maxLen: 8},
		{name: "short-one", label: rawShortDictLabel(1, 1), maxLen: 8, canonical: true},
		{name: "long-one", label: rawLongDictLabel(1, 1, 8), maxLen: 8},
		{name: "same-one", label: rawSameDictLabel(1, 1, 8), maxLen: 8},
		{name: "same-three", label: rawSameDictLabel(0b111, 3, 8), maxLen: 8, canonical: true},
		{name: "short-three-same", label: rawShortDictLabel(0b111, 3), maxLen: 8},
		{name: "long-three-same", label: rawLongDictLabel(0b111, 3, 8), maxLen: 8},
		{name: "short-four-tie", label: rawShortDictLabel(0b1010, 4), maxLen: 8, canonical: true},
		{name: "long-four-tie", label: rawLongDictLabel(0b1010, 4, 8), maxLen: 8},
		{name: "long-five", label: rawLongDictLabel(0b10101, 5, 8), maxLen: 8, canonical: true},
		{name: "short-five", label: rawShortDictLabel(0b10101, 5), maxLen: 8},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parseFixedDictNode(tc.label.EndCell(), tc.maxLen)
			if err != nil {
				t.Fatal(err)
			}
			got, err := node.hasCanonicalLabel(tc.maxLen)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.canonical {
				t.Fatalf("canonical = %t, want %t", got, tc.canonical)
			}
		})
	}
}

func TestDictionaryChangedPathCanonicalizesForkLabel(t *testing.T) {
	t.Run("fixed-set", func(t *testing.T) {
		root := nonCanonicalFixedForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
		dict := root.AsDict(1)
		changed, err := dict.SetBuilderByIntKeyWithMode(big.NewInt(1), BeginCell().MustStoreUInt(0xCC, 8), DictSetModeSet)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("set did not change the dictionary")
		}
		if got := dict.AsCell().BitsSize(); got != 2 {
			t.Fatalf("rebuilt root bits = %d, want canonical hml_short size 2", got)
		}
	})

	t.Run("prefix-set", func(t *testing.T) {
		root := nonCanonicalPrefixForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
		dict := root.AsPrefixDict(1)
		changed, err := dict.SetBuilderBySliceKeyWithMode(BeginCell().MustStoreUInt(1, 1).ToSlice(), BeginCell().MustStoreUInt(0xCC, 8), DictSetModeSet)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("set did not change the prefix dictionary")
		}
		if got := dict.AsCell().BitsSize(); got != 3 {
			t.Fatalf("rebuilt root bits = %d, want canonical hml_short plus constructor size 3", got)
		}
	})
}

func TestDictionaryNoOpPreservesNonCanonicalForkLabel(t *testing.T) {
	root := nonCanonicalFixedForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
	dict := root.AsDict(1)
	changed, err := dict.SetBuilderByIntKeyWithMode(big.NewInt(1), BeginCell().MustStoreUInt(0xCC, 8), DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("add unexpectedly changed an existing key")
	}
	if dict.AsCell() != root {
		t.Fatal("no-op add rebuilt the noncanonical root")
	}
}

func rawShortDictLabel(value uint64, bits uint) *Builder {
	return BeginCell().
		MustStoreUInt(0, 1).
		MustStoreUInt(sameBitsValue(true, bits), bits).
		MustStoreUInt(0, 1).
		MustStoreUInt(value, bits)
}

func rawLongDictLabel(value uint64, bits, maxLen uint) *Builder {
	return BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreUInt(uint64(bits), dictLabelSizeBits(maxLen)).
		MustStoreUInt(value, bits)
}

func rawSameDictLabel(value uint64, bits, maxLen uint) *Builder {
	bit := uint64(0)
	if value != 0 {
		bit = 1
	}
	return BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreUInt(bit, 1).
		MustStoreUInt(uint64(bits), dictLabelSizeBits(maxLen))
}

func nonCanonicalFixedForkRoot(t *testing.T, keyBits uint, values map[int64]uint64) *Cell {
	t.Helper()
	dict := NewDict(keyBits)
	for key, value := range values {
		if err := dict.SetIntKey(big.NewInt(key), BeginCell().MustStoreUInt(value, 8).EndCell()); err != nil {
			t.Fatalf("build fixed dictionary: %v", err)
		}
	}
	canonical := dict.AsCell()
	return rawLongDictLabel(0, 0, keyBits).
		MustStoreRef(canonical.refs[0]).
		MustStoreRef(canonical.refs[1]).
		EndCell()
}

func nonCanonicalPrefixForkRoot(t *testing.T, keyBits uint, values map[int64]uint64) *Cell {
	t.Helper()
	dict := NewPrefixDict(keyBits)
	for key, value := range values {
		keyCell := BeginCell().MustStoreUInt(uint64(key), keyBits).EndCell()
		if err := dict.Set(keyCell, BeginCell().MustStoreUInt(value, 8).EndCell()); err != nil {
			t.Fatalf("build prefix dictionary: %v", err)
		}
	}
	canonical := dict.AsCell()
	return rawLongDictLabel(0, 0, keyBits).
		MustStoreBoolBit(true).
		MustStoreRef(canonical.refs[0]).
		MustStoreRef(canonical.refs[1]).
		EndCell()
}
