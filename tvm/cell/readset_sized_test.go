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

// Entries are sized apart from slots, so they have their own contract: they must
// cover a shard's share of the hint with the skew allowance on top, stay under
// the slot count that decides when the table grows, and be capped like the
// slots.
func TestReadSetEntriesForCoversTheHintedShare(t *testing.T) {
	if got := readSetEntriesFor(0); got != readSetInitialSlots/2 {
		t.Fatalf("empty hint = %d entries, want the lazy default %d", got, readSetInitialSlots/2)
	}

	for _, cells := range []int{1, 100, 10_000, 100_000, readSetMaxPresizedCells} {
		perShard := (cells + readSetShards - 1) / readSetShards
		entries := readSetEntriesFor(cells)
		if entries < perShard {
			t.Fatalf("hint %d gives %d entries for %d cells per shard", cells, entries, perShard)
		}
		if slots := readSetSlotsFor(cells); entries*2 > slots {
			t.Fatalf("hint %d gives %d entries against %d slots, so the slots grow first", cells, entries, slots)
		}
	}

	capped := readSetEntriesFor(readSetMaxPresizedCells)
	if got := readSetEntriesFor(readSetMaxPresizedCells * 64); got != capped {
		t.Fatalf("hint past the cap = %d entries, want the capped %d", got, capped)
	}
}

// Every hint on this path is a capacity and nothing else, and this is the test
// that says so: the same reads through recorders sized four different ways, and
// four different memo estimates, must produce the same proof, the same update
// and the same applied root, bit for bit.
func TestSizingHintsDoNotChangeProducedCells(t *testing.T) {
	source := func() *Cell {
		root := BeginCell().MustStoreUInt(0xfeed, 32)
		for i := range 4 {
			leaf := BeginCell().MustStoreUInt(uint64(i)+1, 32).
				MustStoreRef(BeginCell().MustStoreUInt(uint64(i)*7+3, 64).EndCell()).
				EndCell()
			root.MustStoreRef(leaf)
		}
		return root.EndCell()
	}

	// The destination keeps two of the source's subtrees — which is what gives
	// the update something to prune onto — and rewrites the rest.
	destination := func(from *Cell) *Cell {
		kept, err := from.PeekRef(0)
		if err != nil {
			t.Fatal(err)
		}
		second, err := from.PeekRef(1)
		if err != nil {
			t.Fatal(err)
		}
		return BeginCell().MustStoreUInt(0xfeed, 32).
			MustStoreRef(kept).
			MustStoreRef(second).
			MustStoreRef(BeginCell().MustStoreUInt(0xbeef, 32).EndCell()).
			MustStoreRef(BeginCell().MustStoreUInt(0xcafe, 32).EndCell()).
			EndCell()
	}

	read := func(rs *ReadSet) {
		root := rs.Root()
		slice, err := root.BeginParse()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = slice.LoadUInt(32); err != nil {
			t.Fatal(err)
		}
		for i := 0; slice.RefsNum() > 0; i++ {
			ref, refErr := slice.LoadRefCell()
			if refErr != nil {
				t.Fatal(refErr)
			}
			// Two subtrees are opened and two are left as references, so the
			// frontier — the half sized from the hint rather than from the reads
			// — has something to hold.
			if i >= 2 {
				continue
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

	type shape struct{ cells, memo int }
	var want [3]Hash
	for i, s := range []shape{{0, 0}, {4, 1}, {1 << 10, 1 << 10}, {readSetMaxPresizedCells << 4, 1 << 20}} {
		from := source()
		rs := NewReadSetSized(from, s.cells)
		read(rs)

		// Asked before the update, which is when the collator's size estimator
		// asks it and therefore when the frontier is sized.
		if _, known := rs.Prunable(from.HashKey()); !known {
			t.Fatalf("%+v: the source root is not known to its own recorder", s)
		}

		proof, err := rs.Proof()
		if err != nil {
			t.Fatalf("%+v: proof: %v", s, err)
		}
		update, applied, memoUsed, err := rs.CreateMerkleUpdateAppliedSized(destination(from), s.memo)
		if err != nil {
			t.Fatalf("%+v: update: %v", s, err)
		}
		if memoUsed <= 0 {
			t.Fatalf("%+v: the walk reported memoising %d cells", s, memoUsed)
		}

		got := [3]Hash{proof.HashKey(), update.HashKey(), applied.HashKey()}
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("%+v produced %x/%x/%x, want %x/%x/%x",
				s, got[0][:4], got[1][:4], got[2][:4], want[0][:4], want[1][:4], want[2][:4])
		}
	}
}

// The frontier is sized from the same whole-set hint the read half is, and that
// hint is the PREVIOUS block's read count: it can arrive stale-high, and one
// builder serves collations whose read sets differ by an order of magnitude. A
// presize that follows the hint without a ceiling turns a stale hint into tens
// of megabytes of table that the block being collated will never fill, held for
// as long as the recorder is — and several recorders are live at once.
//
// So the presize is bounded on both dimensions. The slot count is what decides
// when the table grows; the entry arrays are what the memory is actually in (40
// bytes each against 8 for a slot), which is why they are sized to the
// population rather than to the slots, exactly as the read half's are.
func TestReferencedFrontierPresizeIsCapped(t *testing.T) {
	source := BeginCell().MustStoreUInt(0xfeed, 32)
	for i := range 4 {
		source.MustStoreRef(BeginCell().MustStoreUInt(uint64(i)+1, 32).EndCell())
	}
	root := source.EndCell()

	rs := NewReadSet(root)
	// Set past the cap NewReadSetSized clamps to, which is the whole point: the
	// clamp there bounds the read half, and the frontier sizing must carry its
	// own bound rather than inherit one from a caller.
	rs.expected = 2 * readSetMaxPresizedCells

	// One read, so the frontier's own floor — four times what has been read —
	// cannot be what sizes the table.
	if _, err := rs.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}
	child, err := root.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := rs.Prunable(child.HashKey()); !known {
		t.Fatal("a reference of the read root is not on the frontier")
	}

	table := rs.referenced.table.Load()
	if table == nil {
		t.Fatal("the frontier was never sized")
	}
	if len(table.hashes) > referencedMaxPresizedCells {
		t.Fatalf("a hint of %d cells presized the frontier for %d entries, above the %d ceiling",
			rs.expected, len(table.hashes), referencedMaxPresizedCells)
	}
	if len(table.cells) != len(table.hashes) {
		t.Fatalf("the frontier holds %d hashes against %d cells", len(table.hashes), len(table.cells))
	}
	if len(table.slots) > 2*referencedMaxPresizedCells {
		t.Fatalf("a hint of %d cells presized the frontier for %d slots, above the %d ceiling",
			rs.expected, len(table.slots), 2*referencedMaxPresizedCells)
	}
	t.Logf("hint %d cells -> %d slots, %d entries (%d bytes)",
		rs.expected, len(table.slots), len(table.hashes),
		8*len(table.slots)+40*len(table.hashes))
}

// The shape itself, without allocating any of it: proportional to the hint at
// the measured frontier-to-read ratio, never below the four-times-read floor
// that ties it to the block actually being collated, never above the ceiling,
// and never sized so the entries outlive the slots.
func TestReferencedTableForIsProportionalFlooredAndCapped(t *testing.T) {
	for _, tc := range []struct {
		upTo     int64
		expected int
	}{
		{0, 0}, {1, 0}, {40, 0}, {40, 28_000}, {28_000, 28_000}, {40, 100_000},
		{40, readSetMaxPresizedCells}, {40, 64 * readSetMaxPresizedCells},
		{4 * readSetMaxPresizedCells, 0},
	} {
		slots, entries := referencedTableFor(tc.upTo, tc.expected)

		if slots&(slots-1) != 0 {
			t.Fatalf("%+v: %d slots is not a power of two", tc, slots)
		}
		if slots < readSetInitialSlots || entries < readSetInitialSlots/2 {
			t.Fatalf("%+v: %d slots / %d entries is below the lazy default", tc, slots, entries)
		}
		if entries > referencedMaxPresizedCells || slots > 2*referencedMaxPresizedCells {
			t.Fatalf("%+v: %d slots / %d entries is above the ceiling", tc, slots, entries)
		}
		// The slots decide when the table grows, so the entry array must not be
		// the dimension that runs out first: it would double 40 bytes an entry
		// while the slots it was sized against still had room.
		if 2*entries > slots {
			t.Fatalf("%+v: %d entries against %d slots, so the entries run out first", tc, entries, slots)
		}
		// The floor: whatever the hint says, a frontier derived from n read
		// cells must still hold the references those n cells can carry before
		// it grows.
		if want := 2 * int(tc.upTo); want <= referencedMaxPresizedCells && entries < want {
			t.Fatalf("%+v: %d entries is below the %d the read count alone asks for", tc, entries, want)
		}
		// And proportionality: a hint big enough to clear both the floor and the
		// default must size the table to its measured share of that hint, not to
		// the hint itself.
		if hinted := tc.expected * referencedFrontierPercent / 100; hinted > 2*int(tc.upTo) && hinted < referencedMaxPresizedCells && hinted > readSetInitialSlots/2 {
			if entries != hinted {
				t.Fatalf("%+v: %d entries, want the hinted share %d", tc, entries, hinted)
			}
		}
	}

	if referencedFrontierPercent < 53 || referencedFrontierPercent > 100 {
		t.Fatalf("the frontier share %d%% is outside the measured 52.5-69.0%% band with its rounding",
			referencedFrontierPercent)
	}
}
