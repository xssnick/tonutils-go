package cell

import "testing"

func mustBitSlice(t *testing.T, bits string, refs ...*Cell) *Slice {
	t.Helper()
	b := BeginCell()
	for _, ch := range bits {
		switch ch {
		case '0':
			b.MustStoreBoolBit(false)
		case '1':
			b.MustStoreBoolBit(true)
		default:
			t.Fatalf("unexpected bit %q", ch)
		}
	}
	for _, ref := range refs {
		b.MustStoreRef(ref)
	}
	return b.EndCell().BeginParse()
}

func TestSliceOpsWindowing(t *testing.T) {
	refA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	refB := BeginCell().MustStoreUInt(0xB, 4).EndCell()

	t.Run("PeekRefCellAt", func(t *testing.T) {
		sl := mustBitSlice(t, "101100", refA, refB)
		if got, err := sl.PeekRefCellAt(1); err != nil || got != refB {
			t.Fatalf("unexpected ref lookup: got=%v err=%v", got, err)
		}
		if _, err := sl.PeekRefCellAt(-1); err == nil {
			t.Fatal("negative ref index should fail")
		}
		if _, err := sl.PeekRefCellAt(2); err == nil {
			t.Fatal("out-of-range ref index should fail")
		}
	})

	t.Run("OnlyAndSkip", func(t *testing.T) {
		if sl := mustBitSlice(t, "101100", refA, refB); !sl.OnlyFirst(3, 1) || sl.BitsLeft() != 3 || sl.RefsNum() != 1 {
			t.Fatal("OnlyFirst should trim to requested window")
		}
		if sl := mustBitSlice(t, "101100", refA, refB); !sl.SkipFirst(2, 1) || sl.BitsLeft() != 4 || sl.RefsNum() != 1 {
			t.Fatal("SkipFirst should advance requested prefix")
		}
		if sl := mustBitSlice(t, "101100", refA, refB); !sl.OnlyLast(2, 1) || sl.BitsLeft() != 2 || sl.RefsNum() != 1 {
			t.Fatal("OnlyLast should keep requested suffix")
		}
		if sl := mustBitSlice(t, "101100", refA, refB); !sl.SkipLast(2, 1) || sl.BitsLeft() != 4 || sl.RefsNum() != 1 {
			t.Fatal("SkipLast should remove requested suffix")
		}

		if sl := mustBitSlice(t, "10"); sl.OnlyFirst(3, 0) {
			t.Fatal("OnlyFirst should reject oversized bit request")
		}
		if sl := mustBitSlice(t, "10"); sl.SkipFirst(0, -1) {
			t.Fatal("SkipFirst should reject negative refs")
		}
		if sl := mustBitSlice(t, "10"); sl.OnlyLast(0, 1) {
			t.Fatal("OnlyLast should reject missing refs")
		}
		if sl := mustBitSlice(t, "10"); sl.SkipLast(3, 0) {
			t.Fatal("SkipLast should reject oversized bit request")
		}
	})

	t.Run("Subslice", func(t *testing.T) {
		sl := mustBitSlice(t, "101100", refA, refB)
		sub, err := sl.Subslice(1, 0, 3, 1)
		if err != nil {
			t.Fatal(err)
		}
		if sub.BitsLeft() != 3 || sub.RefsNum() != 1 {
			t.Fatalf("unexpected subslice shape: bits=%d refs=%d", sub.BitsLeft(), sub.RefsNum())
		}
		if got := sub.MustLoadSlice(3); len(got) == 0 {
			t.Fatal("subslice should contain requested bits")
		}
		if _, err := mustBitSlice(t, "10").Subslice(1, 0, 5, 0); err == nil {
			t.Fatal("oversized subslice should fail")
		}
	})
}

func TestSliceOpsComparisonsAndCounts(t *testing.T) {
	prefix := mustBitSlice(t, "101")
	full := mustBitSlice(t, "101100")
	suffix := mustBitSlice(t, "100")
	other := mustBitSlice(t, "111")

	if !full.HasPrefix(prefix) {
		t.Fatal("expected prefix match")
	}
	if full.HasPrefix(other) {
		t.Fatal("unexpected prefix match")
	}
	if !prefix.IsPrefixOf(full) || !prefix.IsProperPrefixOf(full) {
		t.Fatal("expected proper prefix relation")
	}
	if !suffix.IsSuffixOf(full) || !suffix.IsProperSuffixOf(full) {
		t.Fatal("expected proper suffix relation")
	}
	if !prefix.BitsEqual(mustBitSlice(t, "101")) {
		t.Fatal("expected bit-equal slices")
	}
	if prefix.BitsEqual(full) {
		t.Fatal("different slices should not be bit-equal")
	}

	if got := prefix.LexCompare(full); got != -1 {
		t.Fatalf("unexpected lex compare result: %d", got)
	}
	if got := full.LexCompare(prefix); got != 1 {
		t.Fatalf("unexpected reverse lex compare result: %d", got)
	}
	if got := prefix.LexCompare(mustBitSlice(t, "101")); got != 0 {
		t.Fatalf("equal slices should compare to zero, got %d", got)
	}
	if got := (*Slice)(nil).LexCompare(mustBitSlice(t, "1")); got != -1 {
		t.Fatalf("nil should sort before non-empty, got %d", got)
	}
	if got := mustBitSlice(t, "").LexCompare(nil); got != 0 {
		t.Fatalf("empty slice should compare equal to nil, got %d", got)
	}

	run := mustBitSlice(t, "111000")
	if got := run.CountLeading(true); got != 3 {
		t.Fatalf("unexpected leading count: %d", got)
	}
	if got := run.CountTrailing(false); got != 3 {
		t.Fatalf("unexpected trailing count: %d", got)
	}

	trimmed := mustBitSlice(t, "1011000")
	if removed := trimmed.RemoveTrailing(); removed != 3 {
		t.Fatalf("unexpected removed trailing zeros: %d", removed)
	}
	if trimmed.BitsLeft() != 3 {
		t.Fatalf("unexpected trimmed size: %d", trimmed.BitsLeft())
	}

	allZero := mustBitSlice(t, "000")
	if removed := allZero.RemoveTrailing(); removed != 3 {
		t.Fatalf("unexpected all-zero trim count: %d", removed)
	}
	if allZero.BitsLeft() != 0 {
		t.Fatalf("all-zero slice should become empty, bits=%d", allZero.BitsLeft())
	}
}

func TestSliceDepth(t *testing.T) {
	leaf := BeginCell().EndCell()
	mid := BeginCell().MustStoreRef(leaf).EndCell()
	top := mustBitSlice(t, "1", mid)
	if got := top.Depth(); got != 2 {
		t.Fatalf("unexpected slice depth: %d", got)
	}
}
