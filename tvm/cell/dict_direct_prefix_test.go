package cell

import (
	"math/big"
	"testing"
)

func directPrefixTestRoot(t *testing.T) *Cell {
	t.Helper()

	dict := NewDict(8)
	for key, value := range map[int64]uint64{
		0xF0: 0x11,
		0xF3: 0x22,
		0xA0: 0x33,
	} {
		if err := dict.SetIntKey(big.NewInt(key), BeginCell().MustStoreUInt(value, 8).EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	return dict.AsCell()
}

func sameDirectPrefixRoot(left, right *Cell) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.HashKey() == right.HashKey()
}

func TestCutPrefixSubdictDirectKeysMatchCellKey(t *testing.T) {
	root := directPrefixTestRoot(t)
	prefixCell := BeginCell().MustStoreUInt(0xF, 4).EndCell()

	for _, removePrefix := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "remove"}[removePrefix], func(t *testing.T) {
			legacy := root.AsDict(8)
			legacyOK, err := legacy.CutPrefixSubdict(prefixCell, removePrefix)
			if err != nil {
				t.Fatalf("cell prefix: %v", err)
			}

			source := BeginCell().MustStoreUInt(0xAF, 8).EndCell().MustBeginParse()
			if err = source.SkipBits(4); err != nil {
				t.Fatal(err)
			}
			bitsBefore, refsBefore := source.BitsLeft(), source.RefsNum()
			bySlice := root.AsDict(8)
			sliceOK, err := bySlice.CutPrefixSubdictBySlice(source, removePrefix)
			if err != nil {
				t.Fatalf("slice prefix: %v", err)
			}
			if source.BitsLeft() != bitsBefore || source.RefsNum() != refsBefore {
				t.Fatal("slice prefix cursor was mutated")
			}

			byInt := root.AsDict(8)
			intOK, err := byInt.CutPrefixSubdictByInt(big.NewInt(0xF), 4, removePrefix)
			if err != nil {
				t.Fatalf("integer prefix: %v", err)
			}

			if sliceOK != legacyOK || intOK != legacyOK {
				t.Fatalf("success flags: cell=%t slice=%t int=%t", legacyOK, sliceOK, intOK)
			}
			if bySlice.GetKeySize() != legacy.GetKeySize() || byInt.GetKeySize() != legacy.GetKeySize() {
				t.Fatalf("key sizes: cell=%d slice=%d int=%d", legacy.GetKeySize(), bySlice.GetKeySize(), byInt.GetKeySize())
			}
			if !sameDirectPrefixRoot(bySlice.AsCell(), legacy.AsCell()) {
				t.Fatal("slice-prefix result differs from cell-prefix result")
			}
			if !sameDirectPrefixRoot(byInt.AsCell(), legacy.AsCell()) {
				t.Fatal("integer-prefix result differs from cell-prefix result")
			}
		})
	}
}

func TestCutPrefixSubdictDirectEmptyAndMissingParity(t *testing.T) {
	root := directPrefixTestRoot(t)

	tests := []struct {
		name   string
		prefix uint64
		bits   uint
	}{
		{name: "empty", prefix: 0, bits: 0},
		{name: "missing", prefix: 0xB, bits: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix := BeginCell().MustStoreUInt(test.prefix, test.bits).EndCell()
			legacy := root.AsDict(8)
			legacyOK, err := legacy.CutPrefixSubdict(prefix, true)
			if err != nil {
				t.Fatal(err)
			}

			direct := root.AsDict(8)
			directOK, err := direct.CutPrefixSubdictByInt(new(big.Int).SetUint64(test.prefix), test.bits, true)
			if err != nil {
				t.Fatal(err)
			}
			if directOK != legacyOK || direct.GetKeySize() != legacy.GetKeySize() || !sameDirectPrefixRoot(direct.AsCell(), legacy.AsCell()) {
				t.Fatal("direct prefix result differs from cell-prefix result")
			}
		})
	}
}

func TestCutPrefixSubdictDirectTraceParity(t *testing.T) {
	root := directPrefixTestRoot(t)
	prefixCell := BeginCell().MustStoreUInt(0xF, 4).EndCell()

	type traceCounts struct {
		loads   int
		creates int
	}
	cutWithTrace := func(t *testing.T, run func(*Dictionary) error) traceCounts {
		t.Helper()

		counts := traceCounts{}
		var trace *Trace
		trace = NewTrace(TraceHooks{
			OnLoad: func(*Cell) {
				counts.loads++
			},
			OnCreate: func() {
				counts.creates++
			},
			OnChild: func(int) *Trace {
				return trace
			},
		})
		if err := run(root.AsDict(8).SetTrace(trace)); err != nil {
			t.Fatal(err)
		}
		return counts
	}

	for _, removePrefix := range []bool{false, true} {
		legacy := cutWithTrace(t, func(dict *Dictionary) error {
			_, err := dict.CutPrefixSubdict(prefixCell, removePrefix)
			return err
		})
		bySlice := cutWithTrace(t, func(dict *Dictionary) error {
			_, err := dict.CutPrefixSubdictBySlice(prefixCell.MustBeginParse(), removePrefix)
			return err
		})
		byInt := cutWithTrace(t, func(dict *Dictionary) error {
			_, err := dict.CutPrefixSubdictByInt(big.NewInt(0xF), 4, removePrefix)
			return err
		})

		if bySlice != legacy || byInt != legacy {
			t.Fatalf("remove=%t trace counts: cell=%+v slice=%+v int=%+v", removePrefix, legacy, bySlice, byInt)
		}
	}
}
