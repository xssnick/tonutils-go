package cell

import (
	"bytes"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readSetBatchTree(tb testing.TB, leaves int) (*Cell, []*Cell) {
	tb.Helper()

	level := make([]*Cell, 0, leaves)
	all := make([]*Cell, 0, leaves+leaves/3+1)
	for i := range leaves {
		leaf := BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
		level = append(level, leaf)
		all = append(all, leaf)
	}
	for len(level) > 1 {
		next := make([]*Cell, 0, (len(level)+3)/4)
		for start := 0; start < len(level); start += 4 {
			end := min(start+4, len(level))
			builder := BeginCell().MustStoreUInt(uint64(start), 32)
			for _, child := range level[start:end] {
				builder.MustStoreRef(child)
			}
			parent := builder.EndCell()
			next = append(next, parent)
			all = append(all, parent)
		}
		level = next
	}
	if len(level) != 1 {
		tb.Fatal("batch tree has no root")
	}
	return level[0], all
}

func readSetHashCounts(cells []*Cell) map[Hash]int {
	counts := make(map[Hash]int, len(cells))
	for _, c := range cells {
		counts[c.HashKey()]++
	}
	return counts
}

func assertReadSetBatchParity(t *testing.T, single, batch *ReadSet, singleCallbacks, batchCallbacks []*Cell) {
	t.Helper()

	if single.Size() != batch.Size() {
		t.Fatalf("batch size = %d, single size = %d", batch.Size(), single.Size())
	}
	wantHashes := readSetHashCounts(single.Cells())
	gotHashes := readSetHashCounts(batch.Cells())
	if !maps.Equal(gotHashes, wantHashes) {
		t.Fatal("batch and single read sets contain different hashes")
	}
	if !maps.Equal(readSetHashCounts(batchCallbacks), readSetHashCounts(singleCallbacks)) {
		t.Fatal("batch and single callbacks observed different first billed reads")
	}

	wantProof, err := single.Proof()
	if err != nil {
		t.Fatalf("single proof: %v", err)
	}
	gotProof, err := batch.Proof()
	if err != nil {
		t.Fatalf("batch proof: %v", err)
	}
	if !bytes.Equal(ToBOCWithFlags([]*Cell{gotProof}, false), ToBOCWithFlags([]*Cell{wantProof}, false)) {
		t.Fatal("RecordMany changed proof bytes")
	}
}

func TestReadSetRecordManyMatchesIndividualRecord(t *testing.T) {
	root, tree := readSetBatchTree(t, 512)
	input := make([]*Cell, 0, len(tree)*2+2)
	input = append(input, nil)
	for i := len(tree) - 1; i >= 0; i-- {
		input = append(input, tree[i])
		if i%7 == 0 {
			input = append(input, tree[i])
		}
	}

	for _, useBatchCallback := range []bool{false, true} {
		name := "single_callback_fallback"
		if useBatchCallback {
			name = "batch_callback"
		}
		t.Run(name, func(t *testing.T) {
			single := NewReadSetSized(root, len(tree))
			batch := NewReadSetSized(root, len(tree))

			var singleCallbacks, batchCallbacks []*Cell
			single.SetRecordCallback(func(c *Cell) {
				singleCallbacks = append(singleCallbacks, c)
			})
			batch.SetRecordCallback(func(c *Cell) {
				batchCallbacks = append(batchCallbacks, c)
			})
			if useBatchCallback {
				batch.SetRecordManyCallback(func(cells []*Cell) {
					batchCallbacks = append(batchCallbacks, cells...)
				})
			}

			// Exercise promotion as well as insertion: an unbilled cell has
			// already entered the proof set but its first billed record must still
			// fire exactly one callback in both paths.
			for i := 0; i < len(tree); i += 11 {
				single.RecordUnbilled(tree[i])
				batch.RecordUnbilled(tree[i])
			}
			for _, c := range input {
				single.Record(c)
			}
			batch.RecordMany(input)

			assertReadSetBatchParity(t, single, batch, singleCallbacks, batchCallbacks)
		})
	}
}

func TestReadSetRecordManyCallbacksRunOutsideShardLocks(t *testing.T) {
	root, tree := readSetBatchTree(t, 64)
	rs := NewReadSetSized(root, len(tree))
	var callbackRan atomic.Int64
	rs.SetRecordManyCallback(func([]*Cell) {
		// Size takes every read-set shard. Calling it here would deadlock if the
		// batch callback still held the shard it just populated.
		if rs.Size() == 0 {
			t.Error("callback ran before its records were published")
		}
		callbackRan.Add(1)
	})

	done := make(chan struct{})
	go func() {
		rs.RecordMany(tree)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RecordMany callback is blocked on a read-set shard lock")
	}
	if callbackRan.Load() == 0 {
		t.Fatal("RecordMany did not invoke its callback")
	}
}

func TestReadSetConcurrentRecordManyBillsEveryCellOnce(t *testing.T) {
	root, tree := readSetBatchTree(t, 1024)
	rs := NewReadSetSized(root, len(tree))
	for i := 0; i < len(tree); i += 2 {
		rs.RecordUnbilled(tree[i])
	}

	var (
		billed    atomic.Int64
		duplicate atomic.Bool
		seen      sync.Map
	)
	record := func(c *Cell) {
		if _, loaded := seen.LoadOrStore(c.HashKey(), struct{}{}); loaded {
			duplicate.Store(true)
		}
		billed.Add(1)
	}
	rs.SetRecordCallback(record)
	rs.SetRecordManyCallback(func(cells []*Cell) {
		for _, c := range cells {
			record(c)
		}
	})

	var wait sync.WaitGroup
	for worker := range 12 {
		wait.Go(func() {
			start := worker * 97 % len(tree)
			rotated := append(append(make([]*Cell, 0, len(tree)), tree[start:]...), tree[:start]...)
			if worker%3 == 0 {
				for _, c := range rotated {
					rs.Record(c)
				}
				return
			}
			rs.RecordMany(rotated)
		})
	}
	wait.Wait()

	if duplicate.Load() {
		t.Fatal("a first billed read was reported by more than one concurrent recorder")
	}
	if got := int(billed.Load()); got != len(tree) {
		t.Fatalf("callbacks billed %d cells, want %d", got, len(tree))
	}
	if got := rs.Size(); got != len(tree) {
		t.Fatalf("read set holds %d cells, want %d", got, len(tree))
	}
}

func TestReadSetRecordManySingleCallbackDoesNotAllocateBatchWindow(t *testing.T) {
	root, tree := readSetBatchTree(t, 256)
	onRecord := func(*Cell) {}

	individual := testing.AllocsPerRun(20, func() {
		rs := NewReadSetSized(root, len(tree))
		rs.SetRecordCallback(onRecord)
		for _, c := range tree {
			rs.Record(c)
		}
	})
	fallback := testing.AllocsPerRun(20, func() {
		rs := NewReadSetSized(root, len(tree))
		rs.SetRecordCallback(onRecord)
		rs.RecordMany(tree)
	})

	if fallback != individual {
		t.Fatalf("single-callback RecordMany allocated %.0f objects, individual Record allocated %.0f", fallback, individual)
	}
}

func BenchmarkReadSetRecordMany(b *testing.B) {
	root, tree := readSetBatchTree(b, 4096)

	for _, mode := range []string{"individual", "batch"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rs := NewReadSetSized(root, len(tree))
				if mode == "batch" {
					rs.RecordMany(tree)
					continue
				}
				for _, c := range tree {
					rs.Record(c)
				}
			}
		})
	}
}
