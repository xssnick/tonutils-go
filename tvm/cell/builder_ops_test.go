package cell

import "testing"

type builderOpsObserver struct {
	created int
}

func (o *builderOpsObserver) OnCellLoad(_ Hash) {}

func (o *builderOpsObserver) OnCellCreate() {
	o.created++
}

func (o *builderOpsObserver) OnRef(_ TraceNode, _ int) TraceNode {
	return 0
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
		sl := cl.BeginParse()
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
		obs := &builderOpsObserver{}
		specBuilder.observer = obs

		specCell, err := specBuilder.EndCellSpecial(true)
		if err != nil {
			t.Fatal(err)
		}
		if obs.created != 1 {
			t.Fatalf("expected observer notification, got %d", obs.created)
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
}
