package cell

import (
	"fmt"
	"sync"
	"testing"
)

var benchmarkHotReadSetSink *ReadSet

// Group actual finalized cells by shard before timing. Each lane can then own
// one shard, separating contention on unrelated shard state from lock contention
// between writers of the same shard. Unique payloads produce distinct hashes;
// the final Size checks also detect an accidentally repeated input workload.
func readSetHotpathCells(tb testing.TB, perShard int) [readSetShards][]*Cell {
	tb.Helper()
	var groups [readSetShards][]*Cell
	for i := range groups {
		groups[i] = make([]*Cell, 0, perShard)
	}
	remaining := readSetShards * perShard
	for id := uint64(0); remaining > 0; id++ {
		c := BeginCell().MustStoreUInt(id, 64).EndCell()
		hash := c.HashKey()
		shard := int(hash[0]) & (readSetShards - 1)
		if len(groups[shard]) < perShard {
			groups[shard] = append(groups[shard], c)
			remaining--
		}
	}
	return groups
}

func readSetHotpathFlatten(groups [readSetShards][]*Cell, perShard int) []*Cell {
	cells := make([]*Cell, 0, readSetShards*perShard)
	for i := range perShard {
		for _, group := range groups {
			cells = append(cells, group[i])
		}
	}
	return cells
}

// A batched benchmark's ns/op and B/op cover the whole named batch. Report the
// number of calls and a time per call as well, so worker counts and fill sizes
// can be compared without mistaking a larger batch for a slower Record call.
func readSetHotpathReportBatch(b *testing.B, calls int) {
	b.ReportMetric(float64(calls), "records/op")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/(float64(b.N)*float64(calls)), "ns/record")
}

func BenchmarkReadSetHotPath(b *testing.B) {
	const count = 1024
	groups := readSetHotpathCells(b, 2*count/readSetShards)
	cells := readSetHotpathFlatten(groups, count/readSetShards)
	hashes := make([]Hash, count)
	missing := make([]Hash, count)
	for i, c := range cells {
		hashes[i] = c.HashKey()
		missing[i] = groups[i%readSetShards][count/readSetShards+i/readSetShards].HashKey()
	}
	set := NewReadSetSized(nil, count)
	for _, c := range cells {
		set.Record(c)
	}
	if set.Size() != count {
		b.Fatal("steady-state fixture did not record every distinct cell")
	}

	for _, hit := range []bool{true, false} {
		name, queries := "contains_hit", hashes
		if !hit {
			name, queries = "contains_miss", missing
		}
		b.Run(name, func(b *testing.B) {
			var found *Cell
			var ok bool
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				found, ok = set.Contains(queries[i&(count-1)])
				i++
			}
			if ok != hit || (found != nil) != hit {
				b.Fatalf("Contains returned cell=%v, found=%t, want hit=%t", found, ok, hit)
			}
		})
	}
	b.Run("record_hit", func(b *testing.B) {
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			set.Record(cells[i&(count-1)])
			i++
		}
		if got := set.Size(); got != count {
			b.Fatalf("repeated Record changed size to %d", got)
		}
	})
	b.Run("record_unbilled_hit", func(b *testing.B) {
		unbilled := NewReadSetSized(nil, count)
		for _, c := range cells {
			unbilled.RecordUnbilled(c)
		}
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			unbilled.RecordUnbilled(cells[i&(count-1)])
			i++
		}
		if got := unbilled.Size(); got != count {
			b.Fatalf("repeated RecordUnbilled changed size to %d", got)
		}
	})
	for _, detached := range []bool{false, true} {
		name := "inert_active"
		if detached {
			name = "inert_detached"
		}
		b.Run(name, func(b *testing.B) {
			rs := NewReadSet(nil)
			if detached {
				rs.Detach()
			}
			var inert bool
			b.ReportAllocs()
			for b.Loop() {
				inert = rs.Inert()
			}
			if inert != detached {
				b.Fatalf("Inert=%t, want %t", inert, detached)
			}
		})
	}
	b.Run("size", func(b *testing.B) {
		var size int
		b.ReportAllocs()
		for b.Loop() {
			size = set.Size()
		}
		if size != count {
			b.Fatalf("Size=%d, want %d", size, count)
		}
	})
}

func BenchmarkReadSetSmallSet(b *testing.B) {
	groups := readSetHotpathCells(b, 1)
	cells := readSetHotpathFlatten(groups, 1)
	b.Run("empty", func(b *testing.B) {
		var last *ReadSet
		b.ReportAllocs()
		for b.Loop() {
			last = NewReadSet(nil)
		}
		benchmarkHotReadSetSink = last
		if last.Size() != 0 {
			b.Fatal("new read set is not empty")
		}
	})
	b.Run("record8", func(b *testing.B) {
		var last *ReadSet
		b.ReportAllocs()
		for b.Loop() {
			last = NewReadSet(nil)
			for _, c := range cells {
				last.Record(c)
			}
		}
		benchmarkHotReadSetSink = last
		if got := last.Size(); got != len(cells) {
			b.Fatalf("recorded %d distinct cells, want %d", got, len(cells))
		}
		readSetHotpathReportBatch(b, len(cells))
	})
}

// These cases include constructor/presize allocation: B/op is the complete
// footprint of a new set containing count distinct cells, excluding the cells
// themselves. The parallel cases below isolate already-presized writes.
func BenchmarkReadSetPresizedDistinct(b *testing.B) {
	for _, count := range []int{64, 32768} {
		groups := readSetHotpathCells(b, count/readSetShards)
		cells := readSetHotpathFlatten(groups, count/readSetShards)
		for _, billed := range []bool{true, false} {
			name := "billed"
			if !billed {
				name = "unbilled"
			}
			b.Run(fmt.Sprintf("%s/%d", name, count), func(b *testing.B) {
				var last *ReadSet
				b.ReportAllocs()
				for b.Loop() {
					last = NewReadSetSized(nil, count)
					if billed {
						for _, c := range cells {
							last.Record(c)
						}
					} else {
						for _, c := range cells {
							last.RecordUnbilled(c)
						}
					}
				}
				benchmarkHotReadSetSink = last
				if got := last.Size(); got != count {
					b.Fatalf("recorded %d distinct cells, want %d", got, count)
				}
				readSetHotpathReportBatch(b, count)
			})
		}
	}
}

func BenchmarkReadSetPromoteFullShard(b *testing.B) {
	const batch = 64
	const entries = readSetInitialSlots / 2
	groups := readSetHotpathCells(b, entries)
	cells := groups[0]
	sets := make([]*ReadSet, batch)
	b.ReportAllocs()
	for b.Loop() {
		// One timer boundary covers 64 independent promotions. Each shard is
		// exactly full before its first promotion; subsequent promotions of the
		// same set would conceal an unnecessary first-promotion table growth.
		b.StopTimer()
		for i := range sets {
			rs := NewReadSet(nil)
			for _, c := range cells {
				rs.RecordUnbilled(c)
			}
			sets[i] = rs
		}
		b.StartTimer()
		for _, rs := range sets {
			rs.Record(cells[0])
		}
	}
	for _, rs := range sets {
		if got := rs.Size(); got != entries {
			b.Fatalf("promotion changed distinct count to %d, want %d", got, entries)
		}
	}
	readSetHotpathReportBatch(b, batch)
}

type readSetHotpathJob struct {
	set   *ReadSet
	start <-chan struct{}
	ready *sync.WaitGroup
	done  *sync.WaitGroup
}

func BenchmarkReadSetParallelShards(b *testing.B) {
	const perWorker = 4096
	groups := readSetHotpathCells(b, perWorker)
	for _, repeat := range []bool{false, true} {
		mode := "insert"
		if repeat {
			mode = "repeat_hit"
		}
		for _, workers := range []int{1, 2, 4, 8} {
			b.Run(fmt.Sprintf("%s/workers%d", mode, workers), func(b *testing.B) {
				jobs := make([]chan readSetHotpathJob, workers)
				var exited, ready, done sync.WaitGroup
				for lane := range jobs {
					jobs[lane] = make(chan readSetHotpathJob)
					exited.Add(1)
					go func(input <-chan readSetHotpathJob, cells []*Cell) {
						defer exited.Done()
						for job := range input {
							job.ready.Done()
							<-job.start
							for _, c := range cells {
								job.set.Record(c)
							}
							job.done.Done()
						}
					}(jobs[lane], groups[lane])
				}
				var rs *ReadSet
				if repeat {
					rs = NewReadSetSized(nil, readSetShards*perWorker)
					for lane := range workers {
						for _, c := range groups[lane] {
							rs.Record(c)
						}
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					if !repeat {
						// Each writer targets one shard even when workers < 8.
						// Size all shards for perWorker entries so none can grow.
						rs = NewReadSetSized(nil, readSetShards*perWorker)
					}
					start := make(chan struct{})
					ready.Add(workers)
					done.Add(workers)
					for _, input := range jobs {
						input <- readSetHotpathJob{set: rs, start: start, ready: &ready, done: &done}
					}
					ready.Wait()
					b.StartTimer()
					close(start)
					done.Wait()
				}
				for _, input := range jobs {
					close(input)
				}
				exited.Wait()
				if got, want := rs.Size(), workers*perWorker; got != want {
					b.Fatalf("recorded %d distinct cells, want %d", got, want)
				}
				readSetHotpathReportBatch(b, workers*perWorker)
			})
		}
	}
}
