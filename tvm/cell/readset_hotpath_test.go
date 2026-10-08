package cell

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReadSetHotPathGrowsOnlyForNewEntries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		slots, entries int
		grownSlots     int
		grownEntries   int
	}{
		{name: "entry_capacity", slots: 64, entries: 4, grownSlots: 64, grownEntries: 8},
		{name: "slot_load", slots: 8, entries: 16, grownSlots: 16, grownEntries: 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var shard readSetShard
			shard.table.Store(newReadSetTable(tc.slots, tc.entries))
			var cells [5]*Cell
			for i := range cells {
				cells[i] = BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
			}
			for _, c := range cells[:4] {
				if bill, added := shard.insert(c.HashKey(), c, false); bill || !added {
					t.Fatal("an initial unbilled insertion did not add exactly one entry")
				}
			}
			full := shard.table.Load()
			if len(full.slots) != tc.slots || len(full.hashes) != tc.entries {
				t.Fatal("fixture grew before reaching the requested threshold")
			}

			duplicate := cells[0].copy()
			// A writer can miss before taking the mutex, then find that another
			// writer filled the last entry with this same hash. Exercise that
			// authoritative recheck directly, bypassing insert's early probe.
			shard.mu.Lock()
			bill, added := shard.insertLocked(duplicate.HashKey(), duplicate, false, nil)
			shard.mu.Unlock()
			if bill || added || shard.table.Load() != full {
				t.Fatal("a duplicate locked recheck grew the full table")
			}

			if bill, added := shard.insert(duplicate.HashKey(), duplicate, true); !bill || added {
				t.Fatal("promotion must bill once without adding an entry")
			}
			if shard.table.Load() != full {
				t.Fatal("unbilled promotion grew the full table")
			}
			shard.mu.Lock()
			bill, added = shard.insertLocked(duplicate.HashKey(), duplicate, true, nil)
			shard.mu.Unlock()
			if bill || added || shard.table.Load() != full {
				t.Fatal("a repeated billed recheck changed the full table")
			}
			if got, present := shard.lookup(cells[0].HashKey()); !present || got != cells[0] {
				t.Fatal("promotion replaced the first writer's cell")
			}

			if bill, added := shard.insert(cells[4].HashKey(), cells[4], true); !bill || !added {
				t.Fatal("the next distinct billed cell was not inserted")
			}
			grown := shard.table.Load()
			if grown == full || len(grown.slots) != tc.grownSlots || len(grown.hashes) != tc.grownEntries {
				t.Fatalf("growth dimensions: slots=%d entries=%d, want %d and %d",
					len(grown.slots), len(grown.hashes), tc.grownSlots, tc.grownEntries)
			}
			for i, c := range cells {
				if got, present := shard.lookup(c.HashKey()); !present || got != c {
					t.Fatalf("growth changed cell identity at entry %d", i)
				}
				want := readSetUnbilled
				if i == 0 || i == 4 {
					want = readSetBilled
				}
				if got := shard.status(c.HashKey()); got != want {
					t.Fatalf("entry %d billing state = %v, want %v", i, got, want)
				}
			}
			if bill, added := shard.insert(cells[1].HashKey(), cells[1].copy(), true); !bill || added {
				t.Fatal("an unbilled entry lost its pending billing across growth")
			}
			if bill, added := shard.insert(cells[1].HashKey(), cells[1], true); bill || added {
				t.Fatal("an entry was billed twice after growth")
			}
		})
	}
}

func TestReadSetHotPathCounterPrecedesObservedEntries(t *testing.T) {
	const count = 256
	var shard readSetShard
	var recorded atomic.Int64
	var cells [count]*Cell
	for i := range cells {
		cells[i] = BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
	}
	insert := func(c *Cell, billed bool) (bool, bool) {
		shard.mu.Lock()
		bill, added := shard.insertLocked(c.HashKey(), c, billed, &recorded)
		shard.mu.Unlock()
		return bill, added
	}
	if bill, added := insert(cells[0], false); bill || !added || recorded.Load() != 1 {
		t.Fatal("an unbilled insertion must publish one counted entry")
	}
	for _, billed := range []bool{false, true, true, false} {
		before := shard.status(cells[0].HashKey())
		bill, added := insert(cells[0].copy(), billed)
		if added || bill != (billed && before == readSetUnbilled) || recorded.Load() != 1 {
			t.Fatal("duplicate insertion or promotion changed the recorded count")
		}
	}

	var badObservation atomic.Bool
	ready := make(chan struct{})
	done := make(chan struct{})
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		close(ready)
		for i, c := range cells {
			for {
				if got, present := shard.lookup(c.HashKey()); present {
					// Inserts publish in index order. A visible entry must
					// already contribute to the count used by Size, even
					// while its writer has not returned from insertLocked.
					if got != c || recorded.Load() < int64(i+1) {
						badObservation.Store(true)
					}
					break
				}
				select {
				case <-done:
					// The last lookup may have run before the final table
					// publication; acquire the completed generation again.
					if got, present := shard.lookup(c.HashKey()); !present || got != c || recorded.Load() < int64(i+1) {
						badObservation.Store(true)
					}
					return
				default:
					runtime.Gosched()
				}
			}
		}
	}()
	<-ready
	for i, c := range cells[1:] {
		if bill, added := insert(c, true); !bill || !added || recorded.Load() != int64(i+2) {
			t.Error("a distinct insertion did not increment the count exactly once")
			break
		}
		runtime.Gosched()
	}
	close(done)
	<-observed
	if badObservation.Load() {
		t.Fatal("a visible entry preceded its recorded count or lost cell identity")
	}
	if got := recorded.Load(); got != count {
		t.Fatalf("recorded count = %d, want %d", got, count)
	}
}

func TestReadSetHotPathSizeAcrossLifecycle(t *testing.T) {
	var cells [7]*Cell
	for i := range cells {
		cells[i] = BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
	}
	reads := NewReadSet(cells[0])
	billed := make(map[Hash]int)
	reads.SetRecordCallback(func(c *Cell) { billed[c.HashKey()]++ })
	checkSize := func(want int) {
		t.Helper()
		if got := reads.Size(); got != want {
			t.Fatalf("Size = %d, want %d", got, want)
		}
	}
	checkSize(0)
	reads.RecordUnbilled(cells[0])
	checkSize(1)
	reads.Record(cells[0])
	checkSize(1)
	reads.RecordMany([]*Cell{cells[0], cells[1], cells[1]})
	checkSize(2)
	reads.RecordUnbilled(cells[2])
	checkSize(3)

	// Open and close the deferred phase without in-flight writers, as required
	// by callers that need the main table frozen while they build a proof.
	reads.BeginDeferredRecording()
	reads.Record(cells[2])
	reads.Record(cells[3])
	reads.Record(cells[3])
	reads.RecordUnbilled(cells[4])
	reads.Record(cells[4])
	reads.RecordUnbilled(cells[5])
	checkSize(3)
	for _, c := range cells[3:6] {
		if _, present := reads.Contains(c.HashKey()); present {
			t.Fatal("a deferred entry reached the frozen table before flush")
		}
	}
	reads.FlushDeferredRecording()
	checkSize(6)
	for i, c := range cells[:6] {
		want := 1
		if i == 5 {
			want = 0
		}
		if got := billed[c.HashKey()]; got != want {
			t.Fatalf("entry %d billed %d times, want %d", i, got, want)
		}
		if _, present := reads.Contains(c.HashKey()); !present {
			t.Fatalf("entry %d was lost during flush", i)
		}
	}

	reads.Detach()
	checkSize(6)
	reads.Record(cells[6])
	reads.RecordUnbilled(cells[6])
	reads.RecordMany(cells[:])
	checkSize(6)
	reads.Seal()
	checkSize(0)
	reads.Seal()
	checkSize(0)
	if len(billed) != 5 {
		t.Fatalf("closed read set billed additional cells: %d callbacks, want 5", len(billed))
	}
}

func TestReadSetHotPathConcurrentCompletedWritesAndSize(t *testing.T) {
	const count = 512
	const preloaded = count / 2
	const workers = 12
	cells := make([]*Cell, count)
	indices := make(map[Hash]int, count)
	for i := range cells {
		cells[i] = BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
		indices[cells[i].HashKey()] = i
	}
	reads := NewReadSet(cells[0])
	for _, c := range cells[:preloaded] {
		reads.RecordUnbilled(c)
	}
	var billed [count]atomic.Int32
	reads.SetRecordCallback(func(c *Cell) { billed[indices[c.HashKey()]].Add(1) })
	var completed atomic.Int32
	var badSize atomic.Bool
	ready := make(chan struct{})
	done := make(chan struct{})
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		previous := preloaded
		close(ready)
		for {
			// Each completion was published only after its distinct cell's
			// Record returned, so Size must include at least these cells.
			minimum := preloaded + int(completed.Load())
			size := reads.Size()
			if size < minimum || size < previous || size > count {
				badSize.Store(true)
			}
			previous = size
			select {
			case <-done:
				return
			default:
				runtime.Gosched()
			}
		}
	}()
	<-ready
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Go(func() {
			for i := worker; i < count; i += workers {
				reads.Record(cells[i])
				if i >= preloaded {
					completed.Add(1)
				}
				reads.RecordUnbilled(cells[i])
			}
			// Race billed repeats against any workers still inserting or
			// promoting their assigned cells.
			for _, c := range cells {
				reads.Record(c)
			}
		})
	}
	wait.Wait()
	close(done)
	<-observed
	if badSize.Load() {
		t.Fatal("concurrent Size went backwards or omitted completed writes")
	}
	if got := reads.Size(); got != count {
		t.Fatalf("completed Size = %d, want %d", got, count)
	}
	for i := range billed {
		if got := billed[i].Load(); got != 1 {
			t.Fatalf("entry %d billed %d times, want 1", i, got)
		}
	}
}
