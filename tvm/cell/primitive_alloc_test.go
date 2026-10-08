package cell

import "testing"

var primitiveAllocCellSink *Cell

func TestSliceToCellPartialFusedParityAndAllocations(t *testing.T) {
	base := *benchLargeCell().MustBeginParse()
	if err := base.SkipBits(3); err != nil {
		t.Fatal(err)
	}

	want := base.ToBuilder().EndCell()
	got, err := base.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if got.HashKey() != want.HashKey() || got.BitsSize() != want.BitsSize() || got.RefsNum() != want.RefsNum() {
		t.Fatal("partial ToCell differs from builder materialization")
	}

	var materializeErr error
	allocs := testing.AllocsPerRun(1000, func() {
		primitiveAllocCellSink, materializeErr = base.ToCell()
	})
	if materializeErr != nil {
		t.Fatal(materializeErr)
	}
	if allocs != 1 {
		t.Fatalf("partial ToCell allocations = %.0f, want 1", allocs)
	}
}

func TestSliceToCellFusedPreservesSpecialAndTraceSemantics(t *testing.T) {
	body := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	proof, err := CreateMerkleProof(body)
	if err != nil {
		t.Fatal(err)
	}

	full := *proof.MustBeginParse()
	full.forceCopyOnToCell = true
	cloned, err := full.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cloned == proof || cloned.GetType() != proof.GetType() || cloned.LevelMask() != proof.LevelMask() || cloned.HashKey() != proof.HashKey() {
		t.Fatal("forced special-cell copy changed identity-independent semantics")
	}

	creates := 0
	trace := NewTrace(TraceHooks{OnCreate: func() { creates++ }})
	partial := *benchLargeCell().MustBeginParse()
	partial.trace = trace
	if err = partial.SkipBits(1); err != nil {
		t.Fatal(err)
	}
	if _, err = partial.ToCell(); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatalf("create notifications = %d, want 1", creates)
	}
}

func TestAugmentedDictionaryLoadRootExtraIntoMatchesOwned(t *testing.T) {
	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 1), mustTestAugValue(t, 0xAB, 8)); err != nil {
		t.Fatal(err)
	}

	owned, err := dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	var into Slice
	if err = dict.LoadRootExtraInto(&into); err != nil {
		t.Fatal(err)
	}
	if owned.MustLoadUInt(16) != into.MustLoadUInt(16) {
		t.Fatal("LoadRootExtraInto differs from LoadRootExtra")
	}
}

func BenchmarkSliceToCellPartial(b *testing.B) {
	base := benchLargeCell().MustBeginParse()
	if err := base.SkipBits(1); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		c, err := base.ToCell()
		if err != nil {
			b.Fatal(err)
		}
		primitiveAllocCellSink = c
	}
}

func BenchmarkAugmentedDictionaryLoadRootExtra(b *testing.B) {
	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	if err = dict.SetBuilderByBytesKey([]byte{1}, BeginCell().MustStoreUInt(0xAB, 8)); err != nil {
		b.Fatal(err)
	}

	b.Run("Owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			extra, err := dict.LoadRootExtra()
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSliceSink = extra
		}
	})
	b.Run("Into", func(b *testing.B) {
		var extra Slice
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.LoadRootExtraInto(&extra); err != nil {
				b.Fatal(err)
			}
			benchmarkUint64Sink = uint64(extra.BitsLeft())
		}
	})
}
