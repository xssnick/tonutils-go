package cell

import "testing"

type builderOpsTrace struct {
	created int
	loaded  int
}

func TestBuilderOpsAndCellMeta(t *testing.T) {
	t.Run("CanExtendByAndDepth", func(t *testing.T) {
		b := BeginCell()
		if err := b.StoreSameBit(false, 1022); err != nil {
			t.Fatal(err)
		}
		if !b.CanExtendBy(1, 0) {
			t.Fatal("builder should fit one more bit")
		}
		if b.CanExtendBy(2, 0) {
			t.Fatal("builder should reject overflow beyond 1023 bits")
		}

		leaf := BeginCell().EndCell()
		mid := BeginCell().MustStoreRef(leaf).EndCell()
		rootBuilder := BeginCell().MustStoreRef(mid)
		if got := rootBuilder.Depth(); got != 2 {
			t.Fatalf("unexpected builder depth: %d", got)
		}
	})

	t.Run("StoreSameBitAndEndCellSpecial", func(t *testing.T) {
		b := BeginCell()
		if err := b.StoreSameBit(true, 70); err != nil {
			t.Fatal(err)
		}
		if err := b.StoreSameBit(false, 3); err != nil {
			t.Fatal(err)
		}
		if err := b.StoreSameBit(false, 0); err != nil {
			t.Fatal(err)
		}

		cl := b.EndCell()
		sl := cl.MustBeginParse()
		for i := 0; i < 70; i++ {
			if !sl.MustLoadBoolBit() {
				t.Fatalf("bit %d should be true", i)
			}
		}
		for i := 0; i < 3; i++ {
			if sl.MustLoadBoolBit() {
				t.Fatalf("tail bit %d should be false", i)
			}
		}

		specBuilder := BeginCell()
		specBuilder.MustStoreUInt(uint64(LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256)
		tr := &builderOpsTrace{}
		specBuilder.SetTrace(NewTrace(TraceHooks{OnCreate: func() { tr.created++ }}))

		specCell, err := specBuilder.EndCellSpecial(true)
		if err != nil {
			t.Fatal(err)
		}
		if tr.created != 1 {
			t.Fatalf("expected trace notification, got %d", tr.created)
		}
		if !specCell.IsSpecial() {
			t.Fatal("cell should be marked special")
		}
		if specCell.Level() != 0 || specCell.LevelMask() != (LevelMask{}) {
			t.Fatalf("unexpected level metadata: level=%d mask=%v", specCell.Level(), specCell.LevelMask())
		}
	})

	t.Run("StoreSameBitOverflow", func(t *testing.T) {
		b := BeginCell()
		if err := b.StoreSameBit(true, 1024); err == nil {
			t.Fatal("expected overflow for 1024 bits")
		}
	})

	t.Run("ToSliceTraceMatchesEndCellBeginParseSetTrace", func(t *testing.T) {
		tr := &builderOpsTrace{}
		childLoads := 0
		childTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) { childLoads++ }})
		childIdx := -1
		trace := NewTrace(TraceHooks{
			OnCreate: func() { tr.created++ },
			OnLoad:   func(*Cell) { tr.loaded++ },
			OnChild: func(refIdx int) *Trace {
				childIdx = refIdx
				return childTrace
			},
		})

		ref := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		s := BeginCell().
			SetTrace(trace).
			MustStoreUInt(0xCD, 8).
			MustStoreRef(ref).
			ToSlice()

		if tr.created != 1 {
			t.Fatalf("expected one create notification, got %d", tr.created)
		}
		if tr.loaded != 0 {
			t.Fatalf("ToSlice should not load-notify its freshly built cell, got %d", tr.loaded)
		}
		if s.Trace() != trace {
			t.Fatal("slice should keep builder trace")
		}
		if got := s.MustLoadUInt(8); got != 0xCD {
			t.Fatalf("unexpected slice bits: %x", got)
		}

		refCell, err := s.LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		if childIdx != 0 {
			t.Fatalf("unexpected child trace index: %d", childIdx)
		}
		if refCell.Trace() != childTrace {
			t.Fatal("loaded ref cell should carry child trace")
		}
		if _, err = refCell.BeginParse(); err != nil {
			t.Fatal(err)
		}
		if childLoads != 1 {
			t.Fatalf("expected child load notification after parsing ref, got %d", childLoads)
		}
	})
}
