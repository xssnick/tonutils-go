package cell

import (
	"sync"
	"testing"
)

// The deferred window exists so a reader that assumes a stable table — the
// merkle update's source graph — can run while recorders are still
// discovering reads. Three properties carry that: the table is frozen for the
// window, the first-read callback fires exactly once per cell at read time,
// and the flush folds the buffer into the table as if the window never was.
func TestDeferredRecordingFreezesTheTableAndKeepsCallbackOnce(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 32).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	set := NewReadSet(root)

	var mu sync.Mutex
	fired := map[Hash]int{}
	set.SetRecordCallback(func(c *Cell) {
		mu.Lock()
		fired[c.HashKey()]++
		mu.Unlock()
	})

	set.Record(root)
	before := set.Size()

	set.BeginDeferredRecording()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			set.Record(leaf)
			set.Record(root)
		}()
	}
	wg.Wait()
	if got := set.Size(); got != before {
		t.Fatalf("table grew to %d inside the window, want frozen at %d", got, before)
	}
	if fired[leaf.HashKey()] != 1 {
		t.Fatalf("callback fired %d times for the in-window first read, want exactly once", fired[leaf.HashKey()])
	}

	set.FlushDeferredRecording()
	if got := set.Size(); got != before+1 {
		t.Fatalf("table holds %d after flush, want %d", got, before+1)
	}
	if set.RecordedCell(leaf.HashKey()) == nil {
		t.Fatal("flushed cell is not resolvable from the table")
	}
	if fired[leaf.HashKey()] != 1 {
		t.Fatalf("flush replayed the callback: %d fires", fired[leaf.HashKey()])
	}
	if fired[root.HashKey()] != 1 {
		t.Fatalf("a cell recorded before the window fired %d times, want once", fired[root.HashKey()])
	}

	defer func() {
		if recover() == nil {
			t.Fatal("a second BeginDeferredRecording inside an open window did not panic")
		}
	}()
	set.BeginDeferredRecording()
	set.BeginDeferredRecording()
}
