package cell

import (
	"bytes"
	"errors"
	"math/big"
	"math/rand"
	"slices"
	"testing"
)

func TestDictionaryCombineMatchesCanonicalBuild(t *testing.T) {
	tests := []struct {
		name  string
		left  map[uint16]uint16
		right map[uint16]uint16
	}{
		{
			name:  "diverged roots",
			left:  map[uint16]uint16{0x0000: 1, 0x1000: 2},
			right: map[uint16]uint16{0x8000: 3, 0x9000: 4},
		},
		{
			name:  "interleaved prefixes",
			left:  map[uint16]uint16{0x0000: 1, 0x4000: 2, 0x8000: 3, 0xc000: 4},
			right: map[uint16]uint16{0x1000: 5, 0x5000: 6, 0x9000: 7, 0xd000: 8},
		},
		{
			name:  "collisions and unique keys",
			left:  map[uint16]uint16{0x0010: 10, 0x0020: 20, 0x8000: 30, 0xffff: 40},
			right: map[uint16]uint16{0x0010: 1, 0x0030: 2, 0x8000: 3, 0xfffe: 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := mustCombineDict(t, tt.left)
			right := mustCombineDict(t, tt.right)
			merged := left.Copy()

			err := merged.CombineWith(right, combineUint16Values)
			if err != nil {
				t.Fatalf("combine failed: %v", err)
			}

			wantValues := make(map[uint16]uint16, len(tt.left)+len(tt.right))
			for key, value := range tt.left {
				wantValues[key] = value
			}
			for key, value := range tt.right {
				wantValues[key] += value
			}
			want := mustCombineDict(t, wantValues)
			if !bytes.Equal(merged.root.Hash(), want.root.Hash()) {
				t.Fatalf("combined root differs from canonical build\n got: %s\nwant: %s", merged.root.Dump(), want.root.Dump())
			}

			for key, value := range wantValues {
				if got := mustLoadCombineDictValue(t, merged, key); got != value {
					t.Fatalf("key %04x: got %d want %d", key, got, value)
				}
			}
		})
	}
}

func TestDictionaryCombineRandomizedMatchesCanonicalBuild(t *testing.T) {
	rnd := rand.New(rand.NewSource(2026081901))
	for round := 0; round < 500; round++ {
		leftValues := make(map[uint16]uint16)
		rightValues := make(map[uint16]uint16)
		for range rnd.Intn(48) {
			leftValues[uint16(rnd.Uint32())] = uint16(1 + rnd.Intn(1000))
		}
		for range rnd.Intn(48) {
			key := uint16(rnd.Uint32())
			if len(leftValues) != 0 && rnd.Intn(3) == 0 {
				for existing := range leftValues {
					key = existing
					break
				}
			}
			rightValues[key] = uint16(1 + rnd.Intn(1000))
		}

		left := mustCombineDict(t, leftValues)
		right := mustCombineDict(t, rightValues)
		leftRoot, rightRoot := left.root, right.root
		merged := left.Copy()
		if err := merged.CombineWith(right, combineUint16Values); err != nil {
			t.Fatalf("round %d: combine failed: %v", round, err)
		}

		wantValues := make(map[uint16]uint16, len(leftValues)+len(rightValues))
		for key, value := range leftValues {
			wantValues[key] = value
		}
		for key, value := range rightValues {
			wantValues[key] += value
		}
		want := mustCombineDict(t, wantValues)
		if (merged.root == nil) != (want.root == nil) ||
			merged.root != nil && merged.root.HashKey() != want.root.HashKey() {
			t.Fatalf("round %d: combined root differs from canonical build", round)
		}
		if left.root != leftRoot || right.root != rightRoot {
			t.Fatalf("round %d: combine mutated an input dictionary", round)
		}
	}
}

func TestDictionaryCombineEmptyReusesOtherRoot(t *testing.T) {
	left := NewDict(16)
	right := mustCombineDict(t, map[uint16]uint16{1: 10, 2: 20})

	called := false
	if err := left.CombineWith(right, func(_, _ *Slice, _ *Builder) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if called {
		t.Fatal("combiner was called without a collision")
	}
	if left.root != right.root {
		t.Fatal("empty receiver did not reuse the other root")
	}
}

func TestDictionaryCombineEmptyOtherKeepsReceiver(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{1: 10, 2: 20})
	beforeRoot := left.root
	called := false

	if err := left.CombineWith(NewDict(16), func(_, _ *Slice, _ *Builder) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if called {
		t.Fatal("combiner was called for an empty other dictionary")
	}
	if left.root != beforeRoot {
		t.Fatal("receiver root changed after merging an empty dictionary")
	}
}

func TestDictionaryCombineRequiresCombiner(t *testing.T) {
	tests := []struct {
		name  string
		other *Dictionary
	}{
		{name: "empty", other: NewDict(16)},
		{name: "non-empty", other: mustCombineDict(t, map[uint16]uint16{2: 20})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := mustCombineDict(t, map[uint16]uint16{1: 10})
			beforeRoot := left.root

			if err := left.CombineWith(tt.other, nil); err == nil {
				t.Fatal("combine without a callback succeeded")
			}
			if left.root != beforeRoot {
				t.Fatal("receiver changed after missing-callback error")
			}
		})
	}
}

func TestDictionaryCombinePendingTraceErrorKeepsReceiver(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{0x0010: 10, 0x8000: 20})
	right := mustCombineDict(t, map[uint16]uint16{0x0020: 1, 0x9000: 2})
	pendingErr := errors.New("pending trace error")
	trace, loads := pendingErrorAfterLoads(1, pendingErr)
	left.SetTrace(trace)
	beforeRoot := left.root
	called := false

	err := left.CombineWith(right, func(_, _ *Slice, _ *Builder) error {
		called = true
		return nil
	})
	if !errors.Is(err, pendingErr) {
		t.Fatalf("combine error = %v, want %v", err, pendingErr)
	}
	if got := loads(); got != 1 {
		t.Fatalf("load notifications = %d, want 1", got)
	}
	if called {
		t.Fatal("combiner was called after a pending trace error")
	}
	if left.root != beforeRoot {
		t.Fatal("receiver changed after a pending trace error")
	}
}

func TestDictionaryCombineUsesReceiverTrace(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{0x0010: 10, 0x8000: 20})
	right := mustCombineDict(t, map[uint16]uint16{0x0020: 1, 0x9000: 2})
	loads, creates := 0, 0
	var trace *Trace
	trace = NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			loads++
		},
		OnCreate: func() {
			creates++
		},
		OnChild: func(int) *Trace {
			return trace
		},
	})
	left.SetTrace(trace)

	if err := left.CombineWith(right, combineUint16Values); err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if loads == 0 {
		t.Fatal("combine did not report dictionary loads through the receiver trace")
	}
	if creates == 0 {
		t.Fatal("combine did not report rebuilt cells through the receiver trace")
	}
}

func TestDictionaryCombineCallsCollisionsInKeyOrder(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{
		0x0010: 0x0010,
		0x0020: 0x0020,
		0x1000: 0x1000,
		0x8000: 0x8000,
	})
	right := mustCombineDict(t, map[uint16]uint16{
		0x0010: 1,
		0x0020: 1,
		0x1000: 1,
		0x8000: 1,
	})

	var order []uint16
	err := left.CombineWith(right, func(left, right *Slice, dst *Builder) error {
		value, err := left.LoadUInt(16)
		if err != nil {
			return err
		}
		order = append(order, uint16(value))

		other, err := right.LoadUInt(16)
		if err != nil {
			return err
		}
		return dst.StoreUInt(value+other, 16)
	})
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}

	want := []uint16{0x0010, 0x0020, 0x1000, 0x8000}
	if !slices.Equal(order, want) {
		t.Fatalf("collision order = %x, want %x", order, want)
	}
}

func TestDictionaryCombineErrorKeepsReceiver(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{0x0010: 10, 0x8000: 20})
	right := mustCombineDict(t, map[uint16]uint16{0x0010: 1, 0x8000: 2})
	beforeRoot := left.root
	beforeHash := left.root.HashKey()
	wantErr := errors.New("combine failed")
	callbacks := 0

	err := left.CombineWith(right, func(left, right *Slice, dst *Builder) error {
		callbacks++
		if callbacks == 2 {
			return wantErr
		}
		return combineUint16Values(left, right, dst)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("combine error = %v, want %v", err, wantErr)
	}
	if callbacks != 2 {
		t.Fatalf("combiner calls = %d, want 2", callbacks)
	}
	if left.root != beforeRoot || left.root.HashKey() != beforeHash {
		t.Fatal("receiver changed after callback error")
	}
	if got := mustLoadCombineDictValue(t, left, 0x0010); got != 10 {
		t.Fatalf("receiver value changed after callback error: %d", got)
	}
}

func TestDictionaryCombineKeySizeMismatchKeepsReceiver(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{1: 10})
	right := NewDict(8)
	if err := right.Set(BeginCell().MustStoreUInt(1, 8).EndCell(), BeginCell().MustStoreUInt(20, 16).EndCell()); err != nil {
		t.Fatal(err)
	}
	beforeRoot := left.root

	called := false
	err := left.CombineWith(right, func(_, _ *Slice, _ *Builder) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("combine with a different key size succeeded")
	}
	if called {
		t.Fatal("combiner was called after key-size mismatch")
	}
	if left.root != beforeRoot {
		t.Fatal("receiver changed after key-size mismatch")
	}
}

func TestDictionaryCombineLazyDisjointRangesDoNotLoadChildren(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{0x0000: 1, 0x0001: 2})
	right := mustCombineDict(t, map[uint16]uint16{0x8000: 3, 0x8001: 4})
	loader := testLazyLoaderForCellTree(left.root)
	left.root = cellWithLazyRefsFromCell(left.root, loader.LoadCell)

	if err := left.CombineWith(right, combineUint16Values); err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("disjoint merge loaded %d child cells, want 0", loader.calls)
	}
	if !hasLazyPrunedRefs(left.root, map[Hash]struct{}{}) {
		t.Fatal("combined dictionary did not retain lazy unique subtrees")
	}
}

func TestDictionaryCombineLazyLoadsOnlyIntersectingChild(t *testing.T) {
	left := mustCombineDict(t, map[uint16]uint16{0x0000: 1, 0x4000: 2})
	right := mustCombineDict(t, map[uint16]uint16{0x0001: 3})
	residentRoot := left.root
	target := residentRoot.rawRefs()[0]
	untouched := residentRoot.rawRefs()[1]
	loader := testLazyLoaderForCellTree(residentRoot)
	left.root = cellWithLazyRefsFromCell(residentRoot, loader.LoadCell)

	if err := left.CombineWith(right, combineUint16Values); err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if loader.calls != 1 {
		t.Fatalf("merge loaded %d child cells, want 1", loader.calls)
	}
	if loader.lastHash != target.HashKey() {
		t.Fatalf("merge loaded %x, want intersecting child %x", loader.lastHash, target.HashKey())
	}

	kept := left.root.rawRefs()[1]
	if !kept.IsLazy() || kept.HashKey() != untouched.HashKey() {
		t.Fatal("merge did not retain the untouched child as a lazy boundary")
	}
}

func BenchmarkDictionaryCombine(b *testing.B) {
	const entries = 1024
	leftValues := make(map[uint16]uint16, entries)
	rightValues := make(map[uint16]uint16, entries)
	for i := 0; i < entries; i++ {
		leftValues[uint16(i*2)] = uint16(i + 1)
		rightValues[uint16(i*2+1)] = uint16(i + 1)
	}
	left := mustCombineDict(b, leftValues)
	right := mustCombineDict(b, rightValues)

	b.Run("CombineWith", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			merged := left.Copy()
			if err := merged.CombineWith(right, combineUint16Values); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("LoadAllSet", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			merged := left.Copy()
			rightItems, err := right.LoadAll()
			if err != nil {
				b.Fatal(err)
			}
			for _, item := range rightItems {
				value, err := item.Value.ToCell()
				if err != nil {
					b.Fatal(err)
				}
				if err = merged.Set(item.Key.MustToCell(), value); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func combineUint16Values(left, right *Slice, dst *Builder) error {
	a, err := left.LoadUInt(16)
	if err != nil {
		return err
	}
	b, err := right.LoadUInt(16)
	if err != nil {
		return err
	}
	return dst.StoreUInt(a+b, 16)
}

func mustCombineDict(tb testing.TB, values map[uint16]uint16) *Dictionary {
	tb.Helper()

	dict := NewDict(16)
	for key, value := range values {
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(key)), BeginCell().MustStoreUInt(uint64(value), 16).EndCell()); err != nil {
			tb.Fatalf("set key %04x: %v", key, err)
		}
	}
	return dict
}

func mustLoadCombineDictValue(tb testing.TB, dict *Dictionary, key uint16) uint16 {
	tb.Helper()

	value, err := dict.LoadValueByIntKey(new(big.Int).SetUint64(uint64(key)))
	if err != nil {
		tb.Fatalf("load key %04x: %v", key, err)
	}
	loaded, err := value.LoadUInt(16)
	if err != nil {
		tb.Fatalf("load value for key %04x: %v", key, err)
	}
	if value.BitsLeft() != 0 || value.RefsNum() != 0 {
		tb.Fatalf("value for key %04x has trailing data", key)
	}
	return uint16(loaded)
}
