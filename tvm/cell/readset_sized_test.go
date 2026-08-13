package cell

import "testing"

// A presized recorder must record exactly what a lazily grown one records: the
// hint changes where the entries live, never which cells are in the set nor the
// order the frontier is derived in.
func TestNewReadSetSizedRecordsTheSameCells(t *testing.T) {
	build := func() *Cell {
		leaves := make([]*Cell, 0, 4)
		for i := range 4 {
			leaves = append(leaves, BeginCell().MustStoreUInt(uint64(i)+1, 32).EndCell())
		}
		root := BeginCell().MustStoreUInt(0xfeed, 32)
		for _, leaf := range leaves {
			root.MustStoreRef(leaf)
		}
		return root.EndCell()
	}

	readAll := func(rs *ReadSet) {
		root := rs.Root()
		slice, err := root.BeginParse()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = slice.LoadUInt(32); err != nil {
			t.Fatal(err)
		}
		for slice.RefsNum() > 0 {
			ref, refErr := slice.LoadRefCell()
			if refErr != nil {
				t.Fatal(refErr)
			}
			refSlice, parseErr := ref.BeginParse()
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if _, err = refSlice.LoadUInt(32); err != nil {
				t.Fatal(err)
			}
		}
	}

	lazy := NewReadSet(build())
	readAll(lazy)

	for _, hint := range []int{-1, 0, 1, 1 << 10, readSetMaxPresizedCells << 4} {
		sized := NewReadSetSized(build(), hint)
		readAll(sized)

		if sized.Size() != lazy.Size() {
			t.Fatalf("hint %d: recorded %d cells, want %d", hint, sized.Size(), lazy.Size())
		}
		for _, c := range collectReadSetCells(lazy) {
			if _, found := sized.Contains(c.HashKey()); !found {
				t.Fatalf("hint %d: cell %x is missing from the presized set", hint, c.Hash()[:4])
			}
		}
	}
}

func collectReadSetCells(rs *ReadSet) []*Cell {
	var cells []*Cell
	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		if table := shard.table.Load(); table != nil {
			cells = append(cells, table.cells[:shard.used]...)
		}
		shard.mu.Unlock()
	}
	return cells
}

// The hint must never make a shard start smaller than the lazy default, and a
// hint past the cap must be clamped rather than honoured.
func TestReadSetSlotsForIsBoundedAndMonotonic(t *testing.T) {
	if got := readSetSlotsFor(0); got != readSetInitialSlots {
		t.Fatalf("empty hint = %d slots, want the lazy default %d", got, readSetInitialSlots)
	}
	if got := readSetSlotsFor(-5); got != readSetInitialSlots {
		t.Fatalf("negative hint = %d slots, want the lazy default %d", got, readSetInitialSlots)
	}

	previous := 0
	for _, cells := range []int{1, 100, 10_000, 100_000, readSetMaxPresizedCells} {
		slots := readSetSlotsFor(cells)
		if slots < previous {
			t.Fatalf("hint %d shrank the table from %d to %d slots", cells, previous, slots)
		}
		if slots&(slots-1) != 0 {
			t.Fatalf("hint %d produced %d slots, which is not a power of two", cells, slots)
		}
		// A shard sized for its share of the hint must not grow on the way
		// there: insert doubles the table once used*2 reaches len(slots).
		perShard := (cells + readSetShards - 1) / readSetShards
		if perShard*2 >= slots {
			t.Fatalf("hint %d gives %d slots, which still grows at %d entries per shard", cells, slots, perShard)
		}
		previous = slots
	}

	capped := readSetSlotsFor(readSetMaxPresizedCells)
	if got := readSetSlotsFor(readSetMaxPresizedCells * 64); got != capped {
		t.Fatalf("hint past the cap = %d slots, want the capped %d", got, capped)
	}
}
